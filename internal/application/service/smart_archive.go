package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

var (
	ErrArchiveNotFound     = errors.New("smart archive record not found")
	ErrArchiveInvalidFile  = errors.New("smart archive supports PDF, Word, Excel, JPG, PNG and WEBP files")
	ErrArchiveInvalidState = errors.New("smart archive record is not in a valid state")
	ErrArchivePermission   = errors.New("smart archive permission denied")
	// ErrArchiveImageOCRNeedsReview is deliberately distinct from a failed
	// parser error. The source image is already durable, but without a usable
	// vision model (or recognizable OCR text) no fields or reminders may be
	// fabricated. The document is therefore kept in the review queue.
	ErrArchiveImageOCRNeedsReview = errors.New("image OCR requires review")
	// ErrArchiveManagedMirrorMismatch protects the destructive archive cascade:
	// a KnowledgeID is deleted only when it is the tenant's managed Smart
	// Archive mirror for this exact archive document.
	ErrArchiveManagedMirrorMismatch = errors.New("managed smart archive mirror identity mismatch")
)

type smartArchiveService struct {
	repo           interfaces.ArchiveRepository
	files          interfaces.FileService
	reader         interfaces.DocumentReader
	kbs            interfaces.KnowledgeBaseService
	kbRepo         interfaces.KnowledgeBaseRepository
	knowledge      interfaces.KnowledgeService
	parseArtifacts interfaces.DocumentParseArtifactRepository
	models         interfaces.ModelService
	members        interfaces.TenantMemberService
	tenantRepo     interfaces.TenantRepository
	task           interfaces.TaskEnqueuer
	resources      interfaces.ResourceCatalog
	// imageOCR is injectable for focused tests and keeps the image branch
	// independent from the rest of the document parser. Production instances
	// use ocrArchiveImage below.
	imageOCR func(context.Context, storedArchiveUpload) (string, error)
	// reminderWakeup lets a newly-created or edited reminder wake the durable
	// scheduler immediately. It is only an optimization: the scheduler also
	// performs a bounded compensation scan, so a lost wake signal can never
	// make a persisted reminder disappear.
	reminderWakeup chan struct{}
}

func NewSmartArchiveService(repo interfaces.ArchiveRepository, files interfaces.FileService, reader interfaces.DocumentReader, kbs interfaces.KnowledgeBaseService, kbRepo interfaces.KnowledgeBaseRepository, knowledge interfaces.KnowledgeService, models interfaces.ModelService, members interfaces.TenantMemberService, tenantRepo interfaces.TenantRepository, parseArtifacts interfaces.DocumentParseArtifactRepository, task interfaces.TaskEnqueuer, resources interfaces.ResourceCatalog) interfaces.SmartArchiveService {
	service := &smartArchiveService{repo: repo, files: files, reader: reader, kbs: kbs, kbRepo: kbRepo, knowledge: knowledge, parseArtifacts: parseArtifacts, models: models, members: members, tenantRepo: tenantRepo, task: task, resources: resources, reminderWakeup: make(chan struct{}, 1)}
	service.imageOCR = service.ocrArchiveImage
	return service
}

// ReminderWakeups returns a best-effort wake channel for the scheduler. The
// channel is deliberately lossy and buffered: durable reminder rows remain
// the source of truth and the scheduler's compensation scan covers missed
// signals.
func (s *smartArchiveService) ReminderWakeups() <-chan struct{} {
	if s.reminderWakeup == nil {
		s.reminderWakeup = make(chan struct{}, 1)
	}
	return s.reminderWakeup
}

func (s *smartArchiveService) signalReminderScheduleChanged() {
	if s.reminderWakeup == nil {
		s.reminderWakeup = make(chan struct{}, 1)
	}
	select {
	case s.reminderWakeup <- struct{}{}:
	default:
	}
}

func (s *smartArchiveService) GetSettings(ctx context.Context, tenantID uint64) (*types.ArchiveSettings, error) {
	row, err := s.repo.GetSettings(ctx, tenantID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = &types.ArchiveSettings{TenantID: tenantID}
		_ = s.ensureManagedKnowledgeBase(ctx, tenantID, row)
		if err := s.repo.SaveSettings(ctx, row); err != nil {
			return nil, err
		}
		// Continue through the normal repair path below. A newly-created settings
		// row may already have a managed KB and should use the same artifact
		// backfill safeguards as an existing row.
		err = nil
	}
	if row != nil && row.ManagedKnowledgeBaseID == "" {
		_ = s.ensureManagedKnowledgeBase(ctx, tenantID, row)
		if row.ManagedKnowledgeBaseID != "" {
			_ = s.repo.SaveSettings(ctx, row)
		}
	}
	if row != nil && row.ManagedKnowledgeBaseID != "" {
		_ = s.ensureManagedKnowledgeBaseEmbedding(ctx, tenantID, row.ManagedKnowledgeBaseID)
		// Repair older archive rows that predate the shared parser artifact and
		// link them to the managed KB without re-running OCR when extracted text
		// is already available.
		_ = s.syncUnlinkedManagedKnowledge(ctx, tenantID, row.ManagedKnowledgeBaseID)
	}
	return row, err
}

func (s *smartArchiveService) ensureManagedKnowledgeBase(ctx context.Context, tenantID uint64, settings *types.ArchiveSettings) error {
	if settings == nil || settings.ManagedKnowledgeBaseID != "" || s.kbs == nil {
		return nil
	}
	if _, ok := types.TenantInfoFromContext(ctx); !ok {
		return nil
	}
	embeddingModelID, err := s.defaultEmbeddingModelID(ctx)
	if err != nil {
		return err
	}
	kb, err := s.kbs.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
		Name:             "合同智能档案",
		Type:             types.KnowledgeBaseTypeDocument,
		Description:      types.ManagedSmartArchiveKnowledgeBaseMarker + " ChatSwitch 智能档案托管知识库；请通过智能档案模块管理导入与字段。",
		EmbeddingModelID: embeddingModelID,
	})
	if err != nil {
		return err
	}
	settings.ManagedKnowledgeBaseID = kb.ID
	return nil
}

func (s *smartArchiveService) defaultEmbeddingModelID(ctx context.Context) (string, error) {
	if s.models == nil {
		return "", errors.New("smart archive embedding model service is unavailable")
	}
	models, err := s.models.ListModels(ctx)
	if err != nil {
		return "", err
	}
	fallback := ""
	for _, model := range models {
		if model == nil || model.Type != types.ModelTypeEmbedding || model.Status != types.ModelStatusActive {
			continue
		}
		if fallback == "" {
			fallback = model.ID
		}
		if model.IsDefault {
			return model.ID, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", errors.New("no active embedding model is configured for smart archive")
}

func (s *smartArchiveService) ensureManagedKnowledgeBaseEmbedding(ctx context.Context, tenantID uint64, kbID string) error {
	if s.kbRepo == nil || strings.TrimSpace(kbID) == "" {
		return nil
	}
	kb, err := s.kbRepo.GetKnowledgeBaseByIDAndTenant(ctx, kbID, tenantID)
	if err != nil {
		return err
	}
	legacyName := strings.TrimSpace(kb.Name) == "合同与资产档案"
	if legacyName {
		kb.Name = "合同智能档案"
		kb.UpdatedAt = time.Now()
	}
	if strings.TrimSpace(kb.EmbeddingModelID) != "" {
		if legacyName {
			return s.kbRepo.UpdateKnowledgeBase(ctx, kb)
		}
		return nil
	}
	modelID, err := s.defaultEmbeddingModelID(ctx)
	if err != nil {
		return err
	}
	kb.EmbeddingModelID = modelID
	kb.UpdatedAt = time.Now()
	return s.kbRepo.UpdateKnowledgeBase(ctx, kb)
}

func (s *smartArchiveService) UpdateSettings(ctx context.Context, tenantID uint64, input *types.ArchiveSettings) (*types.ArchiveSettings, error) {
	row, err := s.GetSettings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if input != nil {
		if strings.TrimSpace(input.Timezone) != "" {
			row.Timezone = strings.TrimSpace(input.Timezone)
		}
		if input.ExtractionModelID != "" {
			row.ExtractionModelID = strings.TrimSpace(input.ExtractionModelID)
		}
		if input.ExtractionVersion != "" {
			row.ExtractionVersion = strings.TrimSpace(input.ExtractionVersion)
		}
		if input.TrashRetentionDays > 0 && input.TrashRetentionDays <= 3650 {
			row.TrashRetentionDays = input.TrashRetentionDays
		}
	}
	if _, err := time.LoadLocation(row.Timezone); err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}
	if err := s.repo.SaveSettings(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

type storedArchiveUpload struct {
	name, mime string
	size       int64
	data       []byte
}

func (s *smartArchiveService) Import(ctx context.Context, tenantID uint64, userID string, uploads []*types.ArchiveUpload) (*types.ArchiveImportBatch, error) {
	if len(uploads) == 0 {
		return nil, ErrArchiveInvalidFile
	}
	max := int64(secutils.GetMaxFileSizeMB()) * 1024 * 1024
	stored := make([]storedArchiveUpload, 0, len(uploads))
	for _, upload := range uploads {
		if upload == nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(upload.FileName))
		if !archiveExtensionAllowed(ext) {
			return nil, ErrArchiveInvalidFile
		}
		if upload.Size <= 0 || upload.Size > max {
			return nil, fmt.Errorf("file size must be between 1 byte and %dMB", secutils.GetMaxFileSizeMB())
		}
		if upload.Reader == nil {
			return nil, ErrArchiveInvalidFile
		}
		data, err := io.ReadAll(io.LimitReader(upload.Reader, max+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > max {
			return nil, fmt.Errorf("file exceeds size limit of %dMB", secutils.GetMaxFileSizeMB())
		}
		if err := validateArchiveUploadMIME(ext, upload.MimeType); err != nil {
			return nil, err
		}
		if err := validateArchiveUploadContent(ext, data); err != nil {
			return nil, err
		}
		safe, err := secutils.SafeFileName(upload.FileName)
		if err != nil {
			return nil, ErrArchiveInvalidFile
		}
		stored = append(stored, storedArchiveUpload{name: safe, mime: upload.MimeType, size: int64(len(data)), data: data})
	}
	if len(stored) == 0 {
		return nil, ErrArchiveInvalidFile
	}
	version := types.ArchiveDefaultExtractionVersion
	if settings, settingsErr := s.repo.GetSettings(ctx, tenantID); settingsErr == nil && settings != nil && strings.TrimSpace(settings.ExtractionVersion) != "" {
		version = strings.TrimSpace(settings.ExtractionVersion)
	}
	batch := &types.ArchiveImportBatch{TenantID: tenantID, UserID: userID, Total: len(stored), Status: types.ArchiveImportBatchProcessing}
	if err := s.repo.CreateBatch(ctx, batch); err != nil {
		return nil, err
	}
	// Store each source before returning from the request. The queue payload
	// carries only durable identifiers; workers reopen the source from storage,
	// so multipart/request cancellation cannot strand an in-memory goroutine.
	for _, upload := range stored {
		hashBytes := sha256.Sum256(upload.data)
		hash := hex.EncodeToString(hashBytes[:])
		existing, findErr := s.repo.FindDocumentByHash(ctx, tenantID, hash, version)
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return batch, findErr
		}
		item := &types.ArchiveImportItem{
			TenantID: tenantID, BatchID: batch.ID, FileName: upload.name,
			FileType: strings.ToLower(filepath.Ext(upload.name)), MimeType: upload.mime,
			FileSize: upload.size, FileHash: hash, ExtractionVersion: version,
			Status: types.ArchiveImportItemQueued, AvailableAt: ptrTime(time.Now().UTC()),
		}
		if existing != nil && existing.ID != "" {
			// Import items are unique per source fingerprint. A duplicate upload
			// is represented by the new batch counter only; the original durable
			// item remains the single worker/idempotency record for those bytes.
			if err := s.incrementBatch(ctx, tenantID, batch.ID, true); err != nil {
				return batch, err
			}
			continue
		}

		ref, err := s.files.SaveBytes(ctx, upload.data, tenantID, "smart_archive_"+uuid.NewString()[:12]+strings.ToLower(filepath.Ext(upload.name)), false)
		if err != nil {
			return batch, err
		}
		doc := &types.ArchiveDocument{
			TenantID: tenantID, ImportBatchID: batch.ID, Title: upload.name,
			FileName: upload.name, FileType: strings.ToLower(filepath.Ext(upload.name)),
			FileSize: upload.size, FileHash: hash, FilePath: ref, CreatedBy: userID,
			ExtractionStatus: types.ArchiveExtractionParsing, ExtractionVersion: version,
		}
		if err := s.repo.CreateDocument(ctx, doc); err != nil {
			_ = s.files.DeleteFile(ctx, ref)
			return batch, err
		}
		item.DocumentID, item.FilePath = doc.ID, ref
		// A trashed document no longer participates in the active-document
		// fingerprint index, but its durable queue row is intentionally retained
		// for audit/recovery. Reuse that row for the new document instead of
		// violating the queue fingerprint constraint on re-import.
		existingItem, itemErr := s.repo.GetImportItemByFingerprint(ctx, tenantID, hash, version)
		if itemErr == nil && existingItem != nil && existingItem.ID != "" {
			item.ID = existingItem.ID
			item.CreatedAt = existingItem.CreatedAt
			item.AttemptCount = existingItem.AttemptCount
			item.FailureCounted = false
			item.CompletedAt = nil
			if err := s.repo.UpdateImportItem(ctx, item); err != nil {
				_ = s.repo.HardDeleteDocument(ctx, tenantID, doc.ID)
				_ = s.files.DeleteFile(ctx, ref)
				return batch, err
			}
		} else if errors.Is(itemErr, gorm.ErrRecordNotFound) || itemErr == nil {
			if err := s.repo.CreateImportItem(ctx, item); err != nil {
				_ = s.repo.HardDeleteDocument(ctx, tenantID, doc.ID)
				_ = s.files.DeleteFile(ctx, ref)
				return batch, err
			}
		} else {
			_ = s.repo.HardDeleteDocument(ctx, tenantID, doc.ID)
			_ = s.files.DeleteFile(ctx, ref)
			return batch, itemErr
		}
		if s.resources != nil {
			if err := s.resources.Bind(ctx, ref, types.ResourceOwnerSmartArchive, doc.ID, types.ResourceRelationSourceFile); err != nil {
				_ = s.repo.HardDeleteDocument(ctx, tenantID, doc.ID)
				_ = s.files.DeleteFile(ctx, ref)
				return batch, err
			}
		}
		if err := s.enqueueImportItem(ctx, item); err != nil {
			// Keep the durable item/document in failed state for startup recovery
			// and operator visibility; the source remains available for a retry.
			_ = s.repo.MarkImportItemFailed(ctx, tenantID, item.ID, err.Error(), ptrTime(time.Now().UTC()))
			return batch, err
		}
	}
	if refreshed, err := s.repo.GetBatch(ctx, tenantID, batch.ID); err == nil {
		batch = refreshed
	}
	return batch, nil
}

func ptrTime(value time.Time) *time.Time { return &value }

func (s *smartArchiveService) enqueueImportItem(ctx context.Context, item *types.ArchiveImportItem) error {
	if s.task == nil || item == nil {
		return errors.New("smart archive task queue is unavailable")
	}
	payload, err := json.Marshal(types.ArchiveImportTaskPayload{
		TenantID: item.TenantID, BatchID: item.BatchID, ItemID: item.ID,
		RunID: uuid.NewString(), FileHash: item.FileHash, ExtractionVersion: item.ExtractionVersion,
	})
	if err != nil {
		return err
	}
	queue, ok := types.QueueForTaskType(types.TypeSmartArchiveDocumentProcess)
	if !ok {
		queue = types.QueueDefault
	}
	_, err = s.task.Enqueue(asynq.NewTask(types.TypeSmartArchiveDocumentProcess, payload),
		asynq.Queue(queue), asynq.MaxRetry(2), asynq.Timeout(10*time.Minute),
		asynq.TaskID("smart-archive-"+item.ID))
	return err
}

// ProcessDocument is the durable worker entry point for one archive import
// item. It claims a short lease before reading storage, reuses the shared
// parser artifact pipeline, and reconciles batch counters only through the
// repository's terminal transitions.
func (s *smartArchiveService) ProcessDocument(ctx context.Context, task *asynq.Task) error {
	if task == nil {
		return fmt.Errorf("smart archive task is nil: %w", asynq.SkipRetry)
	}
	var payload types.ArchiveImportTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.TenantID == 0 || strings.TrimSpace(payload.ItemID) == "" {
		return fmt.Errorf("invalid smart archive task payload: %w", asynq.SkipRetry)
	}
	if strings.TrimSpace(payload.RunID) == "" {
		payload.RunID = uuid.NewString()
	}
	now := time.Now().UTC()
	item, err := s.repo.ClaimImportItem(ctx, payload.TenantID, payload.ItemID, payload.RunID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// A completed item or a lease owned by another worker is already being
		// handled. Returning nil keeps duplicate deliveries idempotent.
		return nil
	}
	if err != nil {
		return err
	}
	if item == nil || item.TenantID != payload.TenantID ||
		(payload.BatchID != "" && item.BatchID != payload.BatchID) ||
		(payload.FileHash != "" && item.FileHash != payload.FileHash) ||
		(payload.ExtractionVersion != "" && item.ExtractionVersion != payload.ExtractionVersion) {
		// The task may be a stale delivery for an item that was re-imported
		// after its source was trashed. Never mark the current item permanently
		// failed because an old payload disagrees with its durable fingerprint.
		if item != nil && item.TenantID == payload.TenantID {
			item.Status = types.ArchiveImportItemQueued
			item.RunID, item.ClaimedAt, item.LeaseUntil = "", nil, nil
			item.AvailableAt = ptrTime(now)
			item.ErrorMessage = "stale smart archive task payload"
			if releaseErr := s.repo.UpdateImportItem(ctx, item); releaseErr != nil {
				return releaseErr
			}
		}
		return fmt.Errorf("%w: smart archive task scope mismatch", asynq.SkipRetry)
	}
	doc, err := s.repo.GetDocument(ctx, payload.TenantID, item.DocumentID)
	if errors.Is(err, gorm.ErrRecordNotFound) || doc == nil {
		return s.finishArchiveTaskFailure(ctx, payload.TenantID, payload.ItemID, ErrArchiveNotFound, true)
	}
	if err != nil {
		return s.finishArchiveTaskFailure(ctx, payload.TenantID, payload.ItemID, err, false)
	}
	if doc.ExtractionStatus == types.ArchiveExtractionCompleted || doc.ExtractionStatus == types.ArchiveExtractionReview {
		return s.repo.MarkImportItemCompleted(ctx, payload.TenantID, item.ID, doc.ID)
	}
	filePath := strings.TrimSpace(item.FilePath)
	if filePath == "" {
		filePath = strings.TrimSpace(doc.FilePath)
	}
	if s.files == nil || filePath == "" {
		return s.finishArchiveTaskFailure(ctx, payload.TenantID, payload.ItemID, errors.New("smart archive source file is unavailable"), false)
	}
	file, err := s.files.GetFile(ctx, filePath)
	if err != nil {
		return s.finishArchiveTaskFailure(ctx, payload.TenantID, payload.ItemID, err, false)
	}
	max := int64(secutils.GetMaxFileSizeMB()) * 1024 * 1024
	data, readErr := io.ReadAll(io.LimitReader(file, max+1))
	_ = file.Close()
	if readErr != nil {
		return s.finishArchiveTaskFailure(ctx, payload.TenantID, payload.ItemID, readErr, false)
	}
	if int64(len(data)) > max {
		return s.finishArchiveTaskFailure(ctx, payload.TenantID, payload.ItemID, fmt.Errorf("file exceeds size limit of %dMB", secutils.GetMaxFileSizeMB()), true)
	}
	batchID := item.BatchID
	userID := doc.CreatedBy
	if batchID != "" {
		if batch, batchErr := s.repo.GetBatch(ctx, payload.TenantID, batchID); batchErr == nil && batch != nil {
			userID = batch.UserID
		}
	}
	workerCtx := archiveContextWithTenant(ctx, payload.TenantID)
	if s.tenantRepo != nil {
		if enriched, contextErr := s.archiveTenantContext(workerCtx, payload.TenantID); contextErr == nil {
			workerCtx = enriched
		}
	}
	upload := storedArchiveUpload{name: doc.FileName, mime: item.MimeType, size: int64(len(data)), data: data}
	if err := s.processOneWithDocument(workerCtx, payload.TenantID, userID, batchID, upload, doc); err != nil {
		return s.finishArchiveTaskFailure(ctx, payload.TenantID, payload.ItemID, err, false)
	}
	if err := s.repo.MarkImportItemCompleted(ctx, payload.TenantID, item.ID, doc.ID); err != nil {
		return err
	}
	return nil
}

func (s *smartArchiveService) finishArchiveTaskFailure(ctx context.Context, tenantID uint64, itemID string, taskErr error, terminal bool) error {
	if taskErr == nil {
		taskErr = errors.New("smart archive task failed")
	}
	retryCount, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	// Lite mode carries retry metadata through the shared WeKnora context
	// helper instead of Asynq's private context keys.
	if !retryOK || !maxOK {
		if liteRetry, liteMax, liteOK := types.TaskRetryMetadataFromContext(ctx); liteOK {
			retryCount, maxRetry, retryOK, maxOK = liteRetry, liteMax, true, true
		}
	}
	if !retryOK || !maxOK || maxRetry <= 0 {
		maxRetry = 2
	}
	if !terminal && retryCount < maxRetry {
		if item, getErr := s.repo.GetImportItem(ctx, tenantID, itemID); getErr == nil && item != nil {
			item.Status = types.ArchiveImportItemQueued
			item.RunID, item.ClaimedAt, item.LeaseUntil = "", nil, nil
			item.ErrorMessage = taskErr.Error()
			item.AvailableAt = ptrTime(time.Now().UTC())
			if updateErr := s.repo.UpdateImportItem(ctx, item); updateErr != nil {
				return updateErr
			}
		} else if getErr != nil {
			return getErr
		}
		return taskErr
	}
	if err := s.repo.MarkImportItemFailed(ctx, tenantID, itemID, taskErr.Error(), nil); err != nil {
		return err
	}
	return fmt.Errorf("%w: %v", asynq.SkipRetry, taskErr)
}

// RecoverPendingImports re-arms queued and expired-lease items after a
// restart. The database lease and deterministic task ID make this safe to run
// on every instance; an already-enqueued task is simply left in place.
func (s *smartArchiveService) RecoverPendingImports(ctx context.Context) error {
	if s.task == nil {
		return errors.New("smart archive task queue is unavailable")
	}
	items, err := s.repo.ListPendingImportItems(ctx, time.Now().UTC(), 500)
	if err != nil {
		return err
	}
	var firstErr error
	for _, item := range items {
		if item == nil || item.TenantID == 0 || item.ID == "" {
			continue
		}
		if err := s.enqueueImportItem(ctx, item); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func archiveExtensionAllowed(ext string) bool {
	switch ext {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func archiveImageExtension(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}

func archiveImageMIME(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

func validateArchiveUploadMIME(ext, declared string) error {
	expected := archiveImageMIME(ext)
	declared = strings.ToLower(strings.TrimSpace(declared))
	if separator := strings.IndexByte(declared, ';'); separator >= 0 {
		declared = strings.TrimSpace(declared[:separator])
	}
	if expected == "" || declared == "" || declared == "application/octet-stream" {
		return nil
	}
	// image/jpg is a common browser alias for the standards-based image/jpeg.
	if expected == "image/jpeg" && declared == "image/jpg" {
		return nil
	}
	if declared != expected {
		return ErrArchiveInvalidFile
	}
	return nil
}

// validateArchiveUploadContent uses the file signature rather than trusting a
// browser-provided Content-Type or extension. It intentionally applies only
// to the newly supported image formats; existing Office/PDF validation remains
// delegated to their established readers.
func validateArchiveUploadContent(ext string, data []byte) error {
	expected := archiveImageMIME(ext)
	if expected == "" {
		return nil
	}
	if len(data) == 0 {
		return ErrArchiveInvalidFile
	}
	detected := http.DetectContentType(data)
	// net/http reports WebP as application/octet-stream on some Go versions,
	// so validate the RIFF/WEBP signature explicitly in that one case.
	if expected == "image/webp" {
		if len(data) < 16 {
			return ErrArchiveInvalidFile
		}
		riffSize := int(binary.LittleEndian.Uint32(data[4:8]))
		if string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" ||
			riffSize < 8 || riffSize+8 > len(data) {
			return ErrArchiveInvalidFile
		}
		chunk := string(data[12:16])
		if chunk != "VP8 " && chunk != "VP8L" && chunk != "VP8X" {
			return ErrArchiveInvalidFile
		}
		return nil
	}
	if detected != expected {
		return ErrArchiveInvalidFile
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (expected == "image/jpeg" && format != "jpeg") || (expected == "image/png" && format != "png") {
		return ErrArchiveInvalidFile
	}
	return nil
}

func (s *smartArchiveService) processBatch(ctx context.Context, tenantID uint64, userID, batchID string, uploads []storedArchiveUpload) {
	for _, upload := range uploads {
		if err := s.processOne(ctx, tenantID, userID, batchID, upload); err != nil {
			batch, getErr := s.repo.GetBatch(ctx, tenantID, batchID)
			if getErr == nil {
				batch.Failed++
				batch.UpdatedAt = time.Now()
				if batch.Completed+batch.Failed >= batch.Total {
					batch.Status = "completed"
					if batch.Completed == 0 && batch.Failed > 0 {
						batch.Status = "failed"
					}
				}
				_ = s.repo.UpdateBatch(ctx, batch)
			}
		}
	}
	batch, err := s.repo.GetBatch(ctx, tenantID, batchID)
	if err == nil {
		batch.Status = "completed"
		if batch.Completed == 0 && batch.Failed > 0 {
			batch.Status = "failed"
		}
		batch.UpdatedAt = time.Now()
		_ = s.repo.UpdateBatch(ctx, batch)
	}
}

func (s *smartArchiveService) processOne(ctx context.Context, tenantID uint64, userID, batchID string, upload storedArchiveUpload) error {
	if err := s.processOneWithDocument(ctx, tenantID, userID, batchID, upload, nil); err != nil {
		return err
	}
	return s.incrementBatch(ctx, tenantID, batchID, true)
}

// processOneWithDocument runs the shared extraction pipeline. A nil document
// creates a new import row (the legacy in-process path used by focused tests);
// a non-nil document is an already durable queue item and must not be treated
// as a duplicate of itself.
func (s *smartArchiveService) processOneWithDocument(ctx context.Context, tenantID uint64, userID, batchID string, upload storedArchiveUpload, doc *types.ArchiveDocument) error {
	if doc == nil {
		hashBytes := sha256.Sum256(upload.data)
		hash := hex.EncodeToString(hashBytes[:])
		if existing, err := s.repo.FindDocumentByHash(ctx, tenantID, hash, types.ArchiveDefaultExtractionVersion); err == nil && existing != nil && existing.ID != "" {
			return nil
		}
		ref, err := s.files.SaveBytes(ctx, upload.data, tenantID, "smart_archive_"+uuid.NewString()[:12]+strings.ToLower(filepath.Ext(upload.name)), false)
		if err != nil {
			return err
		}
		doc = &types.ArchiveDocument{TenantID: tenantID, ImportBatchID: batchID, Title: upload.name, FileName: upload.name, FileType: strings.ToLower(filepath.Ext(upload.name)), FileSize: upload.size, FileHash: hash, FilePath: ref, CreatedBy: userID, ExtractionStatus: types.ArchiveExtractionParsing}
		if err := s.repo.CreateDocument(ctx, doc); err != nil {
			_ = s.files.DeleteFile(ctx, ref)
			return err
		}
		if s.resources != nil {
			if err := s.resources.Bind(ctx, ref, types.ResourceOwnerSmartArchive, doc.ID, types.ResourceRelationSourceFile); err != nil {
				_ = s.repo.HardDeleteDocument(ctx, tenantID, doc.ID)
				_ = s.files.DeleteFile(ctx, ref)
				return err
			}
		}
	}
	// Parse/OCR exactly once and persist the normalized result before any
	// downstream consumer starts. The managed Knowledge Base receives the
	// artifact ID below and reuses it for indexing rather than parsing bytes a
	// second time.
	parseResult, parserErr := s.parseForArtifact(archiveContextWithTenant(ctx, tenantID), upload)
	if parserErr != nil {
		if errors.Is(parserErr, ErrArchiveImageOCRNeedsReview) {
			// The original image was persisted before parsing. Keep it available
			// for manual review and do not create guessed fields/evidence or
			// reminder candidates when OCR cannot be performed reliably.
			doc.ExtractionStatus = types.ArchiveExtractionReview
			doc.ErrorMessage = parserErr.Error()
			_ = s.repo.UpdateDocument(ctx, doc)
			return nil
		}
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = parserErr.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return parserErr
	}
	if parseResult == nil {
		return errors.New("document parser returned no result")
	}
	parseResult.MarkdownContent = common.CleanInvalidUTF8(parseResult.MarkdownContent)
	artifact, artifactErr := s.upsertParseArtifact(ctx, tenantID, doc, upload, parseResult)
	if artifactErr != nil {
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = artifactErr.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return artifactErr
	}
	content := parseResult.MarkdownContent
	doc.ExtractedText = content
	doc.ExtractionStatus = types.ArchiveExtractionExtracting
	fields, evidence := extractArchiveFields(doc, content)
	fieldJSON, _ := json.Marshal(fields)
	doc.ExtractedFields = types.JSON(fieldJSON)
	if len(evidence) > 0 {
		if err := s.repo.ReplaceEvidence(ctx, tenantID, doc.ID, evidence); err != nil {
			doc.ExtractionStatus = types.ArchiveExtractionFailed
			doc.ErrorMessage = err.Error()
			_ = s.repo.UpdateDocument(ctx, doc)
			return err
		}
	}
	doc.ExtractionStatus = types.ArchiveExtractionLinking
	if err := s.linkCustomer(ctx, doc, fields); err != nil {
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = err.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return err
	}
	_ = s.linkRelatedDocuments(ctx, doc)
	doc.ExtractionStatus = types.ArchiveExtractionCompleted
	if fields["customer"] == "" && fields["agreement_number"] == "" && len(evidence) == 0 {
		doc.ExtractionStatus = types.ArchiveExtractionReview
		doc.ErrorMessage = "未识别到可验证字段，请检查图片清晰度或点击重新识别"
	}
	if err := s.repo.UpdateDocument(ctx, doc); err != nil {
		return err
	}
	if err := s.detectReminderCandidates(ctx, doc, fields, evidence, userID); err != nil {
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = err.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return err
	}
	// Keep the source in the managed Knowledge Base as well. It is deliberately
	// created only after the parse artifact is durable, so the indexing worker
	// can consume the same text/OCR output. Mirror failures remain best effort;
	// the archive row can be repaired later without parsing the source again.
	if s.knowledge != nil {
		if settings, settingsErr := s.GetSettings(ctx, tenantID); settingsErr == nil && settings.ManagedKnowledgeBaseID != "" {
			mirrorCtx := archiveContextWithTenant(ctx, tenantID)
			if knowledge, knowledgeErr := s.createManagedKnowledge(mirrorCtx, settings.ManagedKnowledgeBaseID, upload, doc.ID, artifact.ID); knowledge != nil {
				doc.KnowledgeID = knowledge.ID
				_ = s.repo.UpdateDocument(ctx, doc)
			} else if knowledgeErr != nil {
				logger.Warnf(ctx, "smart archive: managed knowledge mirror skipped for %s: %v", doc.ID, knowledgeErr)
			}
		}
	}
	return nil
}

func (s *smartArchiveService) linkRelatedDocuments(ctx context.Context, doc *types.ArchiveDocument) error {
	if strings.TrimSpace(doc.AgreementNumber) == "" {
		return nil
	}
	rows, err := s.repo.ListDocuments(ctx, doc.TenantID, doc.AgreementNumber, false)
	if err != nil {
		return err
	}
	for _, other := range rows {
		if other.ID == doc.ID || other.AgreementNumber != doc.AgreementNumber {
			continue
		}
		if err := s.repo.CreateDocumentLink(ctx, &types.ArchiveDocumentLink{TenantID: doc.TenantID, FromDocumentID: doc.ID, ToDocumentID: other.ID, Relation: "same_agreement", LinkStatus: types.ArchiveLinkConfirmed}); err != nil {
			return err
		}
	}
	return nil
}

func (s *smartArchiveService) createManagedKnowledge(ctx context.Context, kbID string, upload storedArchiveUpload, documentID, artifactID string) (*types.Knowledge, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", upload.name)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(upload.data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req := httptest.NewRequest("POST", "/archive/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(int64(len(upload.data)) + 1024); err != nil {
		return nil, err
	}
	files := req.MultipartForm.File["file"]
	if len(files) == 0 {
		return nil, errors.New("managed knowledge multipart file missing")
	}
	var processOverrides *types.KnowledgeProcessOverrides
	if strings.TrimSpace(artifactID) != "" {
		// A durable parse artifact means the managed KB must consume the
		// already-normalized text/OCR result. Image mirrors still need the
		// multimodal flag to pass the legacy image gate, but they do not need to
		// invoke a vision model a second time.
		processOverrides = &types.KnowledgeProcessOverrides{ParseArtifactID: artifactID}
		if archiveImageExtension(filepath.Ext(upload.name)) {
			enableMultimodel := true
			processOverrides.EnableMultimodel = &enableMultimodel
		}
	} else if archiveImageExtension(filepath.Ext(upload.name)) {
		// The managed KB is intentionally read-only and does not expose its
		// parser settings in the UI. Pass the archive's active vision model as a
		// per-import override so image mirrors are accepted even for legacy KBs
		// whose stored VLMConfig is empty. EnableMultimodel is separate from the
		// VLM model setting: the document worker checks both before parsing image
		// files.
		var modelErr error
		processOverrides, modelErr = s.archiveImageProcessOverrides(ctx)
		if modelErr != nil {
			return nil, modelErr
		}
	}
	return s.knowledge.CreateKnowledgeFromFile(withSmartArchiveMutation(ctx), kbID, files[0], map[string]string{"source": "smart_archive", "archive_document_id": documentID}, nil, "", nil, "smart_archive", processOverrides)
}

func (s *smartArchiveService) archiveImageProcessOverrides(ctx context.Context) (*types.KnowledgeProcessOverrides, error) {
	modelID, err := s.archiveImageModelID(ctx)
	if err != nil {
		return nil, err
	}
	enableMultimodel := true
	return &types.KnowledgeProcessOverrides{
		EnableMultimodel: &enableMultimodel,
		VLMConfig:        &types.VLMConfig{Enabled: true, ModelID: modelID},
	}, nil
}

// syncUnlinkedManagedKnowledge repairs best-effort mirrors for documents
// imported before shared parse artifacts existed. It backfills an artifact
// from stored extraction text when possible, links unlinked archive rows and
// repairs linked image rows that failed at the old multimodal gate.
// CreateKnowledgeFromFile is hash idempotent, so repeated settings reads
// cannot create duplicate records.
func (s *smartArchiveService) syncUnlinkedManagedKnowledge(ctx context.Context, tenantID uint64, kbID string) error {
	if s.repo == nil || s.files == nil || s.knowledge == nil || strings.TrimSpace(kbID) == "" {
		return nil
	}
	rows, err := s.repo.ListDocuments(ctx, tenantID, "", false)
	if err != nil {
		return err
	}
	for _, doc := range rows {
		if doc == nil {
			continue
		}
		mirrorCtx := withSmartArchiveMutation(archiveContextWithTenant(ctx, tenantID))
		artifact, _ := s.ensureExistingParseArtifact(ctx, tenantID, doc)
		if doc.KnowledgeID != "" {
			// A previous mirror may have been created before the worker received
			// the explicit multimodal override. Repair only that known terminal
			// image failure; pending or completed knowledge must not be restarted
			// every time the archive page is opened.
			if archiveImageExtension(doc.FileType) && s.knowledge != nil {
				knowledge, knowledgeErr := s.knowledge.GetKnowledgeByID(mirrorCtx, doc.KnowledgeID)
				if knowledgeErr != nil || knowledge == nil || knowledge.ParseStatus != "failed" ||
					!strings.Contains(strings.ToLower(knowledge.ErrorMessage), strings.ToLower(ErrImageNotParse.Error())) {
					continue
				}
				var overrides *types.KnowledgeProcessOverrides
				if artifact != nil {
					enableMultimodel := true
					overrides = &types.KnowledgeProcessOverrides{ParseArtifactID: artifact.ID, EnableMultimodel: &enableMultimodel}
				} else {
					var overrideErr error
					overrides, overrideErr = s.archiveImageProcessOverrides(mirrorCtx)
					if overrideErr != nil {
						logger.Warnf(ctx, "smart archive: cannot repair image mirror %s: %v", doc.ID, overrideErr)
						continue
					}
				}
				if _, reparseErr := s.knowledge.ReparseKnowledge(mirrorCtx, doc.KnowledgeID, overrides); reparseErr != nil {
					logger.Warnf(ctx, "smart archive: image mirror reparse failed for %s: %v", doc.ID, reparseErr)
				}
			}
			continue
		}
		switch doc.ExtractionStatus {
		case types.ArchiveExtractionCompleted, types.ArchiveExtractionReview:
		default:
			continue
		}
		file, fileErr := s.files.GetFile(ctx, doc.FilePath)
		if fileErr != nil {
			logger.Warnf(ctx, "smart archive: cannot read source for mirror backfill %s: %v", doc.ID, fileErr)
			continue
		}
		data, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil {
			logger.Warnf(ctx, "smart archive: cannot read source for mirror backfill %s: %v", doc.ID, readErr)
			continue
		}
		upload := storedArchiveUpload{
			name: doc.FileName,
			mime: archiveImageMIME(doc.FileType),
			size: int64(len(data)),
			data: data,
		}
		if artifact == nil {
			parseResult, parseErr := s.parseForArtifact(mirrorCtx, upload)
			if parseErr == nil && parseResult != nil {
				artifact, parseErr = s.upsertParseArtifact(ctx, tenantID, doc, upload, parseResult)
			}
			if parseErr != nil {
				logger.Warnf(ctx, "smart archive: parse artifact backfill skipped for %s: %v", doc.ID, parseErr)
			}
		}
		artifactID := ""
		if artifact != nil {
			artifactID = artifact.ID
		}
		knowledge, createErr := s.createManagedKnowledge(mirrorCtx, kbID, upload, doc.ID, artifactID)
		if knowledge == nil {
			logger.Warnf(ctx, "smart archive: mirror backfill skipped for %s: %v", doc.ID, createErr)
			continue
		}
		doc.KnowledgeID = knowledge.ID
		if updateErr := s.repo.UpdateDocument(ctx, doc); updateErr != nil {
			logger.Warnf(ctx, "smart archive: mirror link update failed for %s: %v", doc.ID, updateErr)
		}
	}
	return nil
}

func (s *smartArchiveService) incrementBatch(ctx context.Context, tenantID uint64, batchID string, completed bool) error {
	batch, err := s.repo.GetBatch(ctx, tenantID, batchID)
	if err != nil {
		return err
	}
	if completed {
		batch.Completed++
	}
	batch.UpdatedAt = time.Now()
	if batch.Completed+batch.Failed >= batch.Total {
		batch.Status = "completed"
		if batch.Completed == 0 && batch.Failed > 0 {
			batch.Status = "failed"
		}
	}
	return s.repo.UpdateBatch(ctx, batch)
}

func (s *smartArchiveService) parseForArtifact(ctx context.Context, upload storedArchiveUpload) (*types.ReadResult, error) {
	if archiveImageExtension(filepath.Ext(upload.name)) {
		var text string
		var err error
		if s.imageOCR != nil {
			text, err = s.imageOCR(ctx, upload)
		} else {
			text, err = s.ocrArchiveImage(ctx, upload)
		}
		if err != nil {
			return nil, err
		}
		return &types.ReadResult{MarkdownContent: text}, nil
	}
	if s.reader == nil {
		return &types.ReadResult{MarkdownContent: string(upload.data)}, nil
	}
	fileType := strings.TrimPrefix(strings.ToLower(filepath.Ext(upload.name)), ".")
	result, err := s.reader.Read(ctx, &types.ReadRequest{FileContent: upload.data, FileName: upload.name, FileType: fileType, RequestID: uuid.NewString()})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("document parser returned no result")
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	if strings.TrimSpace(result.MarkdownContent) == "" {
		result.MarkdownContent = string(upload.data)
	}
	return result, nil
}

func (s *smartArchiveService) parse(ctx context.Context, upload storedArchiveUpload) (string, error) {
	result, err := s.parseForArtifact(ctx, upload)
	if err != nil || result == nil {
		return "", err
	}
	return result.MarkdownContent, nil
}

// upsertParseArtifact persists the result of the single reader/OCR pass. The
// artifact is intentionally format-neutral so both Smart Archive extraction
// and the managed Knowledge Base indexer can consume the same output.
func (s *smartArchiveService) upsertParseArtifact(ctx context.Context, tenantID uint64, doc *types.ArchiveDocument, upload storedArchiveUpload, result *types.ReadResult) (*types.DocumentParseArtifact, error) {
	if s.parseArtifacts == nil || doc == nil || result == nil {
		return nil, errors.New("document parse artifact repository is unavailable")
	}
	result.MarkdownContent = common.CleanInvalidUTF8(result.MarkdownContent)
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	artifact := &types.DocumentParseArtifact{
		TenantID:         tenantID,
		SourceDocumentID: doc.ID,
		FileHash:         doc.FileHash,
		FileName:         upload.name,
		FileType:         strings.ToLower(filepath.Ext(upload.name)),
		ParserVersion:    types.DocumentParseArtifactVersion,
		MarkdownContent:  result.MarkdownContent,
		Result:           types.JSON(resultJSON),
	}
	if err := s.parseArtifacts.Upsert(ctx, artifact); err != nil {
		return nil, err
	}
	return artifact, nil
}

// ensureExistingParseArtifact backfills a normalized artifact from text that
// was already extracted by an older Smart Archive import. It never re-runs
// OCR, so opening the archive page cannot trigger a second expensive parse.
func (s *smartArchiveService) ensureExistingParseArtifact(ctx context.Context, tenantID uint64, doc *types.ArchiveDocument) (*types.DocumentParseArtifact, error) {
	if s.parseArtifacts == nil || doc == nil || strings.TrimSpace(doc.FileHash) == "" {
		return nil, errors.New("document parse artifact repository is unavailable")
	}
	if artifact, err := s.parseArtifacts.GetByFingerprint(ctx, tenantID, doc.FileHash, types.DocumentParseArtifactVersion); err == nil && artifact != nil {
		return artifact, nil
	}
	if strings.TrimSpace(doc.ExtractedText) == "" {
		return nil, errors.New("no existing extracted text to backfill parse artifact")
	}
	result := &types.ReadResult{MarkdownContent: doc.ExtractedText}
	return s.upsertParseArtifact(ctx, tenantID, doc, storedArchiveUpload{name: doc.FileName, data: []byte(doc.ExtractedText)}, result)
}

func archiveContextWithTenant(ctx context.Context, tenantID uint64) context.Context {
	return context.WithValue(ctx, types.TenantIDContextKey, tenantID)
}

// archiveImageModelID chooses the explicitly configured archive extraction
// model first. When no model is configured, the tenant's default active
// vision-capable model is used, followed by the first active vision model.
// A model must advertise SupportsVision; merely being a chat/VLLM model is not
// sufficient for accepting image bytes.
func (s *smartArchiveService) archiveImageModelID(ctx context.Context) (string, error) {
	if s.models == nil {
		return "", errors.New("no active vision model is configured for smart archive")
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return "", errors.New("smart archive tenant context is unavailable for image OCR")
	}
	configured := ""
	if s.repo != nil {
		if settings, err := s.repo.GetSettings(ctx, tenantID); err == nil && settings != nil {
			configured = strings.TrimSpace(settings.ExtractionModelID)
		}
	}
	if configured != "" {
		model, err := s.models.GetModelByID(ctx, configured)
		if err != nil || model == nil || model.Status != types.ModelStatusActive || !model.Parameters.SupportsVision {
			return "", fmt.Errorf("configured archive extraction model %q is not an active vision model", configured)
		}
		return configured, nil
	}
	models, err := s.models.ListModels(ctx)
	if err != nil {
		return "", err
	}
	fallback := ""
	for _, model := range models {
		if model == nil || model.Status != types.ModelStatusActive || !model.Parameters.SupportsVision {
			continue
		}
		if fallback == "" {
			fallback = model.ID
		}
		if model.IsDefault {
			return model.ID, nil
		}
	}
	if fallback == "" {
		return "", errors.New("no active vision model is configured for smart archive")
	}
	return fallback, nil
}

const (
	// Sending the camera original to a vision model can exceed the model
	// context or HTTP deadline without improving OCR. Keep the original file
	// untouched, but derive a smaller request image for OCR only.
	archiveOCRMaxDimension = 2600
	archiveOCRMaxBytes     = 2 * 1024 * 1024
)

func resizeArchiveOCRImage(src image.Image, maxDimension int) image.Image {
	if src == nil || maxDimension <= 0 {
		return src
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= maxDimension && height <= maxDimension {
		return src
	}
	scale := float64(maxDimension) / float64(width)
	if height > width {
		scale = float64(maxDimension) / float64(height)
	}
	newWidth := maxInt(1, int(float64(width)*scale))
	newHeight := maxInt(1, int(float64(height)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	for y := 0; y < newHeight; y++ {
		sourceY := bounds.Min.Y + (y*height)/newHeight
		for x := 0; x < newWidth; x++ {
			sourceX := bounds.Min.X + (x*width)/newWidth
			dst.Set(x, y, src.At(sourceX, sourceY))
		}
	}
	return dst
}

// archiveJPEGOrientation reads the EXIF orientation tag without adding a
// heavyweight image dependency. Phone cameras commonly store a portrait page
// as landscape pixels plus orientation=6; dropping that tag while resizing
// would send a sideways page to OCR.
func archiveJPEGOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	read16 := func(buf []byte, offset int, little bool) (uint16, bool) {
		if offset < 0 || offset+2 > len(buf) {
			return 0, false
		}
		if little {
			return uint16(buf[offset]) | uint16(buf[offset+1])<<8, true
		}
		return uint16(buf[offset])<<8 | uint16(buf[offset+1]), true
	}
	read32 := func(buf []byte, offset int, little bool) (uint32, bool) {
		if offset < 0 || offset+4 > len(buf) {
			return 0, false
		}
		if little {
			return uint32(buf[offset]) | uint32(buf[offset+1])<<8 | uint32(buf[offset+2])<<16 | uint32(buf[offset+3])<<24, true
		}
		return uint32(buf[offset])<<24 | uint32(buf[offset+1])<<16 | uint32(buf[offset+2])<<8 | uint32(buf[offset+3]), true
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			offset++
			continue
		}
		marker := data[offset+1]
		offset += 2
		if marker == 0xd8 || marker == 0xd9 {
			continue
		}
		if marker == 0xda || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			break
		}
		segmentLength, ok := read16(data, offset, false)
		if !ok || segmentLength < 2 || offset+int(segmentLength) > len(data) {
			break
		}
		segment := data[offset+2 : offset+int(segmentLength)]
		offset += int(segmentLength)
		if marker != 0xe1 || len(segment) < 14 || string(segment[:6]) != "Exif\x00\x00" {
			continue
		}
		tiff := segment[6:]
		little := string(tiff[:2]) == "II"
		if !little && string(tiff[:2]) != "MM" {
			continue
		}
		magic, ok := read16(tiff, 2, little)
		if !ok || magic != 42 {
			continue
		}
		ifdOffset, ok := read32(tiff, 4, little)
		if !ok || int(ifdOffset)+2 > len(tiff) {
			continue
		}
		count, ok := read16(tiff, int(ifdOffset), little)
		if !ok {
			continue
		}
		for index := 0; index < int(count); index++ {
			entry := int(ifdOffset) + 2 + index*12
			if entry+12 > len(tiff) {
				break
			}
			tag, tagOK := read16(tiff, entry, little)
			typeID, typeOK := read16(tiff, entry+2, little)
			valueCount, countOK := read32(tiff, entry+4, little)
			if !tagOK || !typeOK || !countOK || tag != 0x0112 || typeID != 3 || valueCount < 1 {
				continue
			}
			value, valueOK := read16(tiff, entry+8, little)
			if valueOK && value >= 1 && value <= 8 {
				return int(value)
			}
		}
		return 1
	}
	return 1
}

func orientArchiveOCRImage(src image.Image, orientation int) image.Image {
	if src == nil || orientation == 1 {
		return src
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	destWidth, destHeight := width, height
	if orientation == 5 || orientation == 6 || orientation == 7 || orientation == 8 {
		destWidth, destHeight = height, width
	}
	dst := image.NewRGBA(image.Rect(0, 0, destWidth, destHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			destX, destY := x, y
			switch orientation {
			case 2:
				destX = width - 1 - x
			case 3:
				destX, destY = width-1-x, height-1-y
			case 4:
				destY = height - 1 - y
			case 5:
				destX, destY = y, x
			case 6:
				destX, destY = height-1-y, x
			case 7:
				destX, destY = height-1-y, width-1-x
			case 8:
				destX, destY = y, width-1-x
			}
			dst.Set(destX, destY, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
}

func prepareArchiveOCRImage(upload storedArchiveUpload) []byte {
	if len(upload.data) == 0 {
		return upload.data
	}
	// WEBP is intentionally left as-is here. The standard library does not
	// decode it, and the VLM accepts it directly. JPEG/PNG are decoded only to
	// decide whether a camera-sized image should be reduced.
	ext := strings.ToLower(filepath.Ext(upload.name))
	if ext == "" {
		switch strings.ToLower(strings.TrimSpace(upload.mime)) {
		case "image/jpeg", "image/jpg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		}
	}
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		return upload.data
	}
	if len(upload.data) <= archiveOCRMaxBytes {
		if config, _, err := image.DecodeConfig(bytes.NewReader(upload.data)); err == nil && config.Width <= archiveOCRMaxDimension && config.Height <= archiveOCRMaxDimension {
			return upload.data
		}
	}
	src, _, err := image.Decode(bytes.NewReader(upload.data))
	if err != nil {
		return upload.data
	}
	if ext == ".jpg" || ext == ".jpeg" {
		src = orientArchiveOCRImage(src, archiveJPEGOrientation(upload.data))
	}
	resized := resizeArchiveOCRImage(src, archiveOCRMaxDimension)
	var encoded bytes.Buffer
	switch ext {
	case ".jpg", ".jpeg":
		if err := jpeg.Encode(&encoded, resized, &jpeg.Options{Quality: 82}); err != nil {
			return upload.data
		}
	case ".png":
		if err := png.Encode(&encoded, resized); err != nil {
			return upload.data
		}
	}
	if encoded.Len() == 0 || encoded.Len() >= len(upload.data) {
		return upload.data
	}
	return encoded.Bytes()
}

func (s *smartArchiveService) ocrArchiveImage(ctx context.Context, upload storedArchiveUpload) (string, error) {
	modelID, err := s.archiveImageModelID(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrArchiveImageOCRNeedsReview, err)
	}
	model, err := s.models.GetVLMModel(ctx, modelID)
	if err != nil || model == nil {
		if err == nil {
			err = errors.New("vision model returned no client")
		}
		return "", fmt.Errorf("%w: %v", ErrArchiveImageOCRNeedsReview, err)
	}
	ocrData := prepareArchiveOCRImage(upload)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		text, predictErr := model.Predict(ctx, [][]byte{ocrData}, vlmOCRPrompt)
		if predictErr == nil {
			text = normalizeArchiveOCRText(sanitizeOCRText(text))
			if strings.TrimSpace(text) != "" {
				return text, nil
			}
			lastErr = errors.New("OCR returned no recognizable text")
		} else {
			lastErr = predictErr
		}
		if attempt == 0 {
			// A short retry covers transient local VLM queue failures. The
			// request image is already bounded, so this does not resend the
			// multi-megabyte camera original.
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("%w: image OCR failed: %v", ErrArchiveImageOCRNeedsReview, ctx.Err())
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("OCR returned no recognizable text")
	}
	return "", fmt.Errorf("%w: image OCR failed: %v", ErrArchiveImageOCRNeedsReview, lastErr)
}

// normalizeArchiveOCRText removes presentation-only Markdown that the VLM
// adds around OCR values. The archive keeps the text readable and stable for
// field evidence; table pipes, headings and list markers remain intact. The
// source image is still the authoritative original document.
func normalizeArchiveOCRText(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "**", ""))
}

func normalizeArchiveText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

var (
	archiveEntityPrefix = regexp.MustCompile(`(?i)^(?:(?:客户|customer|签收单位(?:名称)?|购买方|采购方|采购人|供应商|甲方|乙方|借用方|出借方|承租方|出租方)(?:[ \t]*[（(][^\n）)]*[）)])?[ \t]*[:：\-—]+)+[ \t]*`)
	archiveEntityLabel  = regexp.MustCompile(`(?i)^(?:客户|customer|采购人|采购方|购买方|供应商|甲方|乙方|借用方|出借方|承租方|出租方)$`)
)

// cleanArchiveEntityName removes presentation markers and role labels from a
// party name. A label by itself (for example "乙方：") is not an entity and
// must never be persisted as a customer merely because an OCR/Markdown line
// break made it look like a regex value.
func cleanArchiveEntityName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("**", "", "__", "", "~~", "", "`", "").Replace(value)
	value = strings.TrimLeft(value, " \t|#>*•·●○■□◆◇-—")
	for {
		cleaned := archiveEntityPrefix.ReplaceAllString(value, "")
		if cleaned == value {
			break
		}
		value = strings.TrimSpace(cleaned)
	}
	value = strings.Trim(value, " \t|#>*•·●○■□◆◇:：;；,，-—")
	if value == "" || archiveEntityLabel.MatchString(value) {
		return ""
	}
	return value
}

// linkCustomer keeps archive imports focused on document/customer records.
// Asset rows and document-to-asset links are legacy data and are no longer
// created during extraction.
func (s *smartArchiveService) linkCustomer(ctx context.Context, doc *types.ArchiveDocument, fields map[string]string) error {
	if customer := strings.TrimSpace(fields["customer"]); customer != "" {
		normalized := normalizeArchiveText(customer)
		row, err := s.repo.FindCustomer(ctx, doc.TenantID, normalized)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = &types.ArchiveCustomer{TenantID: doc.TenantID, Name: customer, Normalized: normalized}
			if err := s.repo.CreateCustomer(ctx, row); err != nil {
				row, err = s.repo.FindCustomer(ctx, doc.TenantID, normalized)
				if err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}
		doc.CustomerID = row.ID
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *smartArchiveService) detectReminderCandidates(ctx context.Context, doc *types.ArchiveDocument, fields map[string]string, evidence []*types.ArchiveFieldEvidence, userID string) error {
	if doc == nil {
		return nil
	}
	byField := make(map[string]*types.ArchiveFieldEvidence, len(evidence))
	for _, row := range evidence {
		if row != nil && row.FieldName != "" {
			byField[row.FieldName] = row
		}
	}
	// Include created candidates as well: if a later re-parse changes the
	// source date, the old suggestion must be marked superseded without ever
	// changing the formal reminder that the user already created.
	existing, err := s.repo.ListReminderCandidates(ctx, doc.TenantID, "")
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	items := []struct {
		field, typ, title, description string
		offset                         int
	}{{"expires_at", string(types.ArchiveReminderExpiry), "合同到期提醒", "请确认合同到期日期并决定是否创建提醒。", 30}, {"payment_at", string(types.ArchiveReminderPayment), "付款节点提醒", "请确认付款日期并决定是否创建提醒。", 0}, {"delivery_at", string(types.ArchiveReminderDelivery), "交付节点提醒", "请确认交付日期并决定是否创建提醒。", 0}, {"renewed_at", string(types.ArchiveReminderRenewal), "续约节点提醒", "请确认续约日期并决定是否创建提醒。", 0}}
	returned := false
	if _, dateErr := parseArchiveDate(fields["returned_at"]); dateErr == nil {
		returned = true
	}
	if doc.DocumentType == types.ArchiveDocumentLoanAgreement {
		items[0] = struct {
			field, typ, title, description string
			offset                         int
		}{"return_due_at", string(types.ArchiveReminderReturn), "设备归还提醒", "请确认设备归还期限并决定是否创建提醒。", 0}
		// Some contracts use a generic expiry label for the return deadline.
		if strings.TrimSpace(fields["return_due_at"]) == "" {
			items[0].field = "expires_at"
		}
	}
	for _, item := range items {
		if doc.DocumentType == types.ArchiveDocumentLoanAgreement && item.typ == string(types.ArchiveReminderReturn) && returned {
			continue
		}
		date, err := parseArchiveFieldDate(item.field, fields[item.field])
		source := byField[item.field]
		if err != nil || source == nil {
			continue
		}
		fingerprintBytes := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s:%s", doc.TenantID, doc.ID, item.typ, item.field, date.Format("2006-01-02"))))
		rule, _ := json.Marshal(map[string]any{"field": item.field, "offset_days": item.offset, "event_at": date.Format("2006-01-02")})
		candidate := &types.ArchiveReminderCandidate{TenantID: doc.TenantID, DocumentID: doc.ID, DocumentTitle: doc.Title, CustomerID: doc.CustomerID, AssigneeID: userID, Type: types.ArchiveReminderType(item.typ), SourceField: item.field, EventAt: date, SuggestedOffsetDays: item.offset, Title: item.title, Description: item.description, Confidence: source.Confidence, Quote: source.Quote, Locator: source.Locator, Rule: types.JSON(rule), NeedsReview: source.Confidence < 0.85, Status: types.ArchiveReminderCandidatePending, Fingerprint: hex.EncodeToString(fingerprintBytes[:]), CreatedBy: userID}
		seen[string(candidate.Type)+":"+candidate.SourceField] = true
		for _, old := range existing {
			if old.DocumentID == doc.ID && old.Type == candidate.Type && old.SourceField == candidate.SourceField && old.Fingerprint != candidate.Fingerprint {
				old.Status = types.ArchiveReminderCandidateSuperseded
				if err := s.repo.UpdateReminderCandidate(ctx, old); err != nil {
					return err
				}
			}
		}
		if err := s.repo.UpsertReminderCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	if doc.DocumentType == types.ArchiveDocumentLoanAgreement && !returned {
		returnField := "expires_at"
		if strings.TrimSpace(fields["return_due_at"]) != "" {
			returnField = "return_due_at"
		}
		date, err := parseArchiveFieldDate(returnField, fields[returnField])
		source := byField[returnField]
		if err == nil && source != nil {
			fingerprintBytes := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s:%s", doc.TenantID, doc.ID, types.ArchiveReminderMissingReturn, returnField, date.Format("2006-01-02"))))
			rule, _ := json.Marshal(map[string]any{"condition": "missing_return", "until": date.Format("2006-01-02")})
			candidate := &types.ArchiveReminderCandidate{TenantID: doc.TenantID, DocumentID: doc.ID, DocumentTitle: doc.Title, CustomerID: doc.CustomerID, AssigneeID: userID, Type: types.ArchiveReminderMissingReturn, SourceField: returnField, EventAt: date, Title: "借用到期后检查归还记录", Description: "如果没有关联归还单，请提醒负责人。请确认是否创建。", Confidence: source.Confidence, Quote: source.Quote, Locator: source.Locator, Rule: types.JSON(rule), NeedsReview: source.Confidence < 0.85, Status: types.ArchiveReminderCandidatePending, Fingerprint: hex.EncodeToString(fingerprintBytes[:]), CreatedBy: userID}
			seen[string(candidate.Type)+":"+candidate.SourceField] = true
			for _, old := range existing {
				if old.DocumentID == doc.ID && old.Type == candidate.Type && old.SourceField == candidate.SourceField && old.Fingerprint != candidate.Fingerprint {
					old.Status = types.ArchiveReminderCandidateSuperseded
					if err := s.repo.UpdateReminderCandidate(ctx, old); err != nil {
						return err
					}
				}
			}
			if err := s.repo.UpsertReminderCandidate(ctx, candidate); err != nil {
				return err
			}
		}
	}
	for _, old := range existing {
		if old.DocumentID == doc.ID && old.Status == types.ArchiveReminderCandidatePending && !seen[string(old.Type)+":"+old.SourceField] {
			old.Status = types.ArchiveReminderCandidateSuperseded
			if err := s.repo.UpdateReminderCandidate(ctx, old); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *smartArchiveService) GetBatch(ctx context.Context, tenantID uint64, id string) (*types.ArchiveImportBatch, error) {
	row, err := s.repo.GetBatch(ctx, tenantID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrArchiveNotFound
	}
	return row, err
}
func (s *smartArchiveService) GetDocument(ctx context.Context, tenantID uint64, id string) (*types.ArchiveDocument, error) {
	row, err := s.repo.GetDocument(ctx, tenantID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrArchiveNotFound
	}
	if err == nil && row != nil && len(row.ExtractedFields) > 0 {
		fields := map[string]string{}
		if json.Unmarshal(row.ExtractedFields, &fields) == nil {
			if value, parseErr := parseArchiveFieldDate("return_due_at", fields["return_due_at"]); parseErr == nil {
				row.ReturnDueAt = &value
			}
		}
	}
	return row, err
}
func (s *smartArchiveService) ListDocuments(ctx context.Context, tenantID uint64, keyword string, archived bool) ([]*types.ArchiveDocument, error) {
	return s.repo.ListDocuments(ctx, tenantID, keyword, archived)
}
func (s *smartArchiveService) ListCustomers(ctx context.Context, tenantID uint64, keyword string) ([]*types.ArchiveCustomer, error) {
	return s.repo.ListCustomers(ctx, tenantID, keyword)
}
func (s *smartArchiveService) UpdateCustomer(ctx context.Context, tenantID uint64, id string, changes map[string]any) (*types.ArchiveCustomer, error) {
	rows, err := s.repo.ListCustomers(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	var row *types.ArchiveCustomer
	for _, item := range rows {
		if item.ID == id {
			row = item
			break
		}
	}
	if row == nil {
		return nil, ErrArchiveNotFound
	}
	if name, ok := changes["name"].(string); ok && strings.TrimSpace(name) != "" {
		row.Name = strings.TrimSpace(name)
		row.Normalized = normalizeArchiveText(row.Name)
	}
	if aliases, ok := changes["aliases"]; ok {
		if data, err := json.Marshal(aliases); err == nil {
			row.Aliases = types.JSON(data)
		}
	}
	if err := s.repo.UpdateCustomer(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}
func (s *smartArchiveService) ListEvidence(ctx context.Context, tenantID uint64, documentID string) ([]*types.ArchiveFieldEvidence, error) {
	return s.repo.ListEvidence(ctx, tenantID, documentID)
}
func (s *smartArchiveService) OpenDocument(ctx context.Context, tenantID uint64, id string) (io.ReadCloser, string, error) {
	row, err := s.GetDocument(ctx, tenantID, id)
	if err != nil {
		return nil, "", err
	}
	file, err := s.openArchiveFile(ctx, row)
	return file, row.FileType, err
}

func (s *smartArchiveService) openArchiveFile(ctx context.Context, doc *types.ArchiveDocument) (io.ReadCloser, error) {
	file, err := s.files.GetFile(ctx, doc.FilePath)
	if err == nil || doc.KnowledgeID == "" || s.knowledge == nil {
		return file, err
	}
	// Older imports briefly replaced the archive-owned resource reference
	// with the managed knowledge file. Let the knowledge service resolve that
	// KB-specific storage backend so those files remain previewable/retryable.
	file, _, knowledgeErr := s.knowledge.GetKnowledgeFile(ctx, doc.KnowledgeID)
	if knowledgeErr != nil {
		return nil, err
	}
	return file, nil
}

// archiveTenantContext supplies the tenant and (when running from the
// scheduler) TenantInfo required by KnowledgeService's destructive cleanup.
// Request handlers already carry TenantInfo; background trash cleanup does
// not, so resolve it through the repository before touching a mirror.
func (s *smartArchiveService) archiveTenantContext(ctx context.Context, tenantID uint64) (context.Context, error) {
	ctx = archiveContextWithTenant(ctx, tenantID)
	if _, ok := types.TenantInfoFromContext(ctx); ok {
		return ctx, nil
	}
	if s.tenantRepo == nil {
		return ctx, errors.New("tenant repository is unavailable for archive cleanup")
	}
	tenant, err := s.tenantRepo.GetTenantByID(ctx, tenantID)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, types.TenantInfoContextKey, tenant), nil
}

func managedArchiveMirrorMatches(knowledge *types.Knowledge, documentID string) bool {
	if knowledge == nil || strings.TrimSpace(documentID) == "" {
		return false
	}
	metadata, err := knowledge.Metadata.Map()
	if err != nil {
		return false
	}
	source, _ := metadata["source"].(string)
	archiveDocumentID, _ := metadata["archive_document_id"].(string)
	return source == "smart_archive" && archiveDocumentID == documentID
}

// deleteManagedKnowledgeMirror removes the managed KB row and all of its
// chunks/vector resources. It refuses to delete arbitrary knowledge IDs: both
// the managed KB ID and the per-document metadata marker must match.
func (s *smartArchiveService) deleteManagedKnowledgeMirror(ctx context.Context, tenantID uint64, doc *types.ArchiveDocument) error {
	if doc == nil || strings.TrimSpace(doc.KnowledgeID) == "" || s.knowledge == nil {
		return nil
	}
	mirrorCtx, err := s.archiveTenantContext(ctx, tenantID)
	if err != nil {
		return err
	}
	mirrorCtx = withSmartArchiveMutation(mirrorCtx)
	knowledge, err := s.knowledge.GetKnowledgeByID(mirrorCtx, doc.KnowledgeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// The mirror may already have been removed by a retry or a prior
		// cleanup. Clearing the dangling link keeps the archive operation
		// idempotent.
		doc.KnowledgeID = ""
		return nil
	}
	if err != nil {
		return err
	}
	settings, err := s.repo.GetSettings(mirrorCtx, tenantID)
	if err != nil {
		return err
	}
	if settings == nil || strings.TrimSpace(settings.ManagedKnowledgeBaseID) == "" || knowledge.KnowledgeBaseID != settings.ManagedKnowledgeBaseID || !managedArchiveMirrorMatches(knowledge, doc.ID) {
		return ErrArchiveManagedMirrorMismatch
	}
	if err := s.knowledge.DeleteKnowledge(mirrorCtx, doc.KnowledgeID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			doc.KnowledgeID = ""
			return nil
		}
		return err
	}
	doc.KnowledgeID = ""
	return nil
}

// restoreManagedKnowledgeMirror rebuilds a deleted mirror from the durable
// archive file and shared parse artifact. It never needs to invoke OCR when
// the artifact from the original import is still present.
func (s *smartArchiveService) restoreManagedKnowledgeMirror(ctx context.Context, tenantID uint64, doc *types.ArchiveDocument) error {
	if doc == nil || s.knowledge == nil {
		return nil
	}
	settings, err := s.repo.GetSettings(ctx, tenantID)
	if err != nil {
		return err
	}
	if settings == nil || strings.TrimSpace(settings.ManagedKnowledgeBaseID) == "" {
		return nil
	}
	mirrorCtx, err := s.archiveTenantContext(ctx, tenantID)
	if err != nil {
		return err
	}
	file, err := s.openArchiveFile(mirrorCtx, doc)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	upload := storedArchiveUpload{name: doc.FileName, mime: archiveImageMIME(doc.FileType), size: int64(len(data)), data: data}
	artifact, artifactErr := s.ensureExistingParseArtifact(mirrorCtx, tenantID, doc)
	if artifactErr != nil {
		// Very old rows may have neither an artifact nor extracted text. Parse
		// once now so the restored mirror still enters the unified pipeline.
		parseResult, parseErr := s.parseForArtifact(mirrorCtx, upload)
		if parseErr != nil {
			return parseErr
		}
		artifact, artifactErr = s.upsertParseArtifact(mirrorCtx, tenantID, doc, upload, parseResult)
		if artifactErr != nil {
			return artifactErr
		}
	}
	knowledge, createErr := s.createManagedKnowledge(mirrorCtx, settings.ManagedKnowledgeBaseID, upload, doc.ID, artifact.ID)
	if knowledge == nil {
		return createErr
	}
	doc.KnowledgeID = knowledge.ID
	return nil
}

func (s *smartArchiveService) UpdateDocument(ctx context.Context, tenantID uint64, id string, changes map[string]any) (*types.ArchiveDocument, error) {
	row, err := s.GetDocument(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if value, ok := changes["title"].(string); ok && strings.TrimSpace(value) != "" {
		row.Title = strings.TrimSpace(value)
	}
	if value, ok := changes["customer"].(string); ok {
		row.CustomerID = strings.TrimSpace(value)
	}
	if value, ok := changes["customer_id"].(string); ok {
		row.CustomerID = strings.TrimSpace(value)
	}
	for key, value := range changes {
		if key == "title" || key == "customer" || key == "customer_id" {
			continue
		}
		if key == "document_type" {
			row.DocumentType = types.ArchiveDocumentType(fmt.Sprint(value))
		}
		if key == "business_type" {
			row.BusinessType = types.ArchiveBusinessType(fmt.Sprint(value))
		}
		if key == "agreement_number" {
			row.AgreementNumber = fmt.Sprint(value)
		}
		if key == "currency" {
			row.Currency = strings.TrimSpace(fmt.Sprint(value))
		}
		if key == "amount" {
			if parsed, parseErr := strconv.ParseFloat(strings.ReplaceAll(fmt.Sprint(value), ",", ""), 64); parseErr == nil {
				row.Amount = parsed
			}
		}
		if key == "signed_at" || key == "effective_at" || key == "expires_at" || key == "return_due_at" || key == "returned_at" || key == "renewed_at" {
			if parsed, parseErr := parseArchiveFieldDate(key, fmt.Sprint(value)); parseErr == nil {
				switch key {
				case "signed_at":
					row.SignedAt = &parsed
				case "effective_at":
					row.EffectiveAt = &parsed
				case "expires_at":
					row.ExpiresAt = &parsed
				case "return_due_at":
					row.ReturnDueAt = &parsed
				case "returned_at":
					row.ReturnedAt = &parsed
				case "renewed_at":
					row.RenewedAt = &parsed
				}
			}
		}
	}
	if err := s.repo.UpdateDocument(ctx, row); err != nil {
		return nil, err
	}
	// Keep the AI candidate evidence for auditability and add a separate
	// manual evidence row for corrected scalar fields.
	if evidence, evidenceErr := s.repo.ListEvidence(ctx, tenantID, row.ID); evidenceErr == nil {
		for key, value := range changes {
			if key == "title" || key == "customer" || key == "customer_id" || key == "document_type" || key == "business_type" || key == "agreement_number" || key == "currency" || key == "amount" || key == "signed_at" || key == "effective_at" || key == "expires_at" || key == "return_due_at" || key == "returned_at" || key == "renewed_at" {
				textValue := strings.TrimSpace(fmt.Sprint(value))
				if textValue == "" {
					continue
				}
				evidence = append(evidence, &types.ArchiveFieldEvidence{TenantID: tenantID, DocumentID: row.ID, FieldName: key, Value: textValue, Confidence: 1, Quote: "Manual correction", LocatorKind: types.ArchiveLocatorText, Locator: types.JSON(`{"manual":true}`), IsManual: true})
			}
		}
		_ = s.repo.ReplaceEvidence(ctx, tenantID, row.ID, evidence)
	}
	// A manually corrected date is the authoritative candidate source. Re-run
	// detection so a changed date supersedes the old pending suggestion while
	// preserving any formal reminder the user already created.
	if extracted := map[string]string{}; json.Unmarshal(row.ExtractedFields, &extracted) == nil {
		for key, value := range changes {
			if key == "signed_at" || key == "effective_at" || key == "expires_at" || key == "return_due_at" || key == "returned_at" || key == "renewed_at" {
				extracted[key] = strings.TrimSpace(fmt.Sprint(value))
			}
		}
		if encoded, marshalErr := json.Marshal(extracted); marshalErr == nil {
			row.ExtractedFields = types.JSON(encoded)
			_ = s.repo.UpdateDocument(ctx, row)
		}
		if evidence, evidenceErr := s.repo.ListEvidence(ctx, tenantID, row.ID); evidenceErr == nil {
			if candidateErr := s.detectReminderCandidates(ctx, row, extracted, evidence, row.CreatedBy); candidateErr != nil {
				return nil, candidateErr
			}
		}
	}
	return s.GetDocument(ctx, tenantID, id)
}

func (s *smartArchiveService) RetryExtraction(ctx context.Context, tenantID uint64, id, userID string) (*types.ArchiveDocument, error) {
	doc, err := s.GetDocument(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if doc.ExtractionStatus != types.ArchiveExtractionFailed && doc.ExtractionStatus != types.ArchiveExtractionReview {
		return nil, ErrArchiveInvalidState
	}
	// Look up the durable queue row before changing the document state. A
	// concurrent retry may already own the item; in that case leave the worker
	// that is doing the extraction undisturbed instead of flipping its document
	// back to "parsing" from a second request.
	existingItem, findErr := s.repo.GetImportItemByFingerprint(ctx, tenantID, doc.FileHash, doc.ExtractionVersion)
	if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return nil, findErr
	}
	if findErr == nil && existingItem != nil && existingItem.Status == types.ArchiveImportItemProcessing {
		return doc, nil
	}
	file, err := s.openArchiveFile(ctx, doc)
	if err != nil {
		return nil, err
	}
	max := int64(secutils.GetMaxFileSizeMB()) * 1024 * 1024
	data, readErr := io.ReadAll(io.LimitReader(file, max+1))
	_ = file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file exceeds size limit of %dMB", secutils.GetMaxFileSizeMB())
	}
	doc.ExtractionStatus = types.ArchiveExtractionParsing
	doc.ErrorMessage = ""
	if err := s.repo.UpdateDocument(ctx, doc); err != nil {
		return nil, err
	}
	item := &types.ArchiveImportItem{
		TenantID: tenantID, BatchID: "", DocumentID: doc.ID, FileName: doc.FileName,
		FileType: doc.FileType, FileSize: int64(len(data)), FileHash: doc.FileHash,
		FilePath: doc.FilePath, ExtractionVersion: doc.ExtractionVersion,
		Status: types.ArchiveImportItemQueued, AvailableAt: ptrTime(time.Now().UTC()),
	}
	// Import-item fingerprints are unique so a retry must reuse the durable
	// row rather than create a second queue record. Detach it from the old
	// batch: an operator-triggered retry is a new attempt, and reusing the old
	// batch would double-count that batch when completion is reconciled.
	if findErr == nil && existingItem != nil {
		item.ID = existingItem.ID
		item.CreatedAt = existingItem.CreatedAt
		item.AttemptCount = existingItem.AttemptCount
		item.CompletedAt = nil
		item.FailureCounted = false
		if err := s.repo.UpdateImportItem(ctx, item); err != nil {
			return nil, err
		}
	} else if findErr == nil || errors.Is(findErr, gorm.ErrRecordNotFound) {
		if err := s.repo.CreateImportItem(ctx, item); err != nil {
			return nil, err
		}
	} else {
		return nil, findErr
	}
	if err := s.enqueueImportItem(ctx, item); err != nil {
		_ = s.repo.MarkImportItemFailed(ctx, tenantID, item.ID, err.Error(), ptrTime(time.Now().UTC()))
		return nil, err
	}
	return doc, nil
}

func (s *smartArchiveService) processRetry(ctx context.Context, tenantID uint64, userID string, upload storedArchiveUpload, doc *types.ArchiveDocument) {
	parseCtx := archiveContextWithTenant(ctx, tenantID)
	parseResult, err := s.parseForArtifact(parseCtx, upload)
	if err != nil {
		if errors.Is(err, ErrArchiveImageOCRNeedsReview) {
			doc.ExtractionStatus = types.ArchiveExtractionReview
			doc.ErrorMessage = err.Error()
			_ = s.repo.UpdateDocument(ctx, doc)
			return
		}
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = err.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return
	}
	if parseResult == nil {
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = "document parser returned no result"
		_ = s.repo.UpdateDocument(ctx, doc)
		return
	}
	parseResult.MarkdownContent = common.CleanInvalidUTF8(parseResult.MarkdownContent)
	artifact, artifactErr := s.upsertParseArtifact(ctx, tenantID, doc, upload, parseResult)
	if artifactErr != nil {
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = artifactErr.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return
	}
	content := parseResult.MarkdownContent
	doc.ExtractedText = content
	doc.ExtractionStatus = types.ArchiveExtractionExtracting
	fields, evidence := extractArchiveFields(doc, content)
	fieldJSON, _ := json.Marshal(fields)
	doc.ExtractedFields = types.JSON(fieldJSON)
	if err := s.repo.ReplaceEvidence(ctx, tenantID, doc.ID, evidence); err != nil {
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = err.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return
	}
	doc.ExtractionStatus = types.ArchiveExtractionLinking
	if err := s.linkCustomer(ctx, doc, fields); err != nil {
		doc.ExtractionStatus = types.ArchiveExtractionFailed
		doc.ErrorMessage = err.Error()
		_ = s.repo.UpdateDocument(ctx, doc)
		return
	}
	_ = s.linkRelatedDocuments(ctx, doc)
	doc.ExtractionStatus = types.ArchiveExtractionCompleted
	if fields["customer"] == "" && fields["agreement_number"] == "" && len(evidence) == 0 {
		doc.ExtractionStatus = types.ArchiveExtractionReview
		doc.ErrorMessage = "未识别到可验证字段，请检查图片清晰度或点击重新识别"
	}
	if err := s.repo.UpdateDocument(ctx, doc); err == nil {
		if candidateErr := s.detectReminderCandidates(ctx, doc, fields, evidence, userID); candidateErr != nil {
			doc.ExtractionStatus = types.ArchiveExtractionFailed
			doc.ErrorMessage = candidateErr.Error()
			_ = s.repo.UpdateDocument(ctx, doc)
			return
		}
		if s.knowledge != nil {
			if doc.KnowledgeID != "" {
				overrides := &types.KnowledgeProcessOverrides{ParseArtifactID: artifact.ID}
				if archiveImageExtension(filepath.Ext(upload.name)) {
					enableMultimodel := true
					overrides.EnableMultimodel = &enableMultimodel
				}
				_, _ = s.knowledge.ReparseKnowledge(withSmartArchiveMutation(parseCtx), doc.KnowledgeID, overrides)
			} else if settings, settingsErr := s.GetSettings(ctx, tenantID); settingsErr == nil && settings.ManagedKnowledgeBaseID != "" {
				if knowledge, createErr := s.createManagedKnowledge(parseCtx, settings.ManagedKnowledgeBaseID, upload, doc.ID, artifact.ID); knowledge != nil {
					doc.KnowledgeID = knowledge.ID
					_ = s.repo.UpdateDocument(ctx, doc)
				} else if createErr != nil {
					logger.Warnf(ctx, "smart archive: managed knowledge mirror retry skipped for %s: %v", doc.ID, createErr)
				}
			}
		}
	}
}

func (s *smartArchiveService) ArchiveDocument(ctx context.Context, tenantID uint64, id string, archive bool) (*types.ArchiveDocument, error) {
	row, err := s.GetDocument(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if archive {
		switch row.ExtractionStatus {
		case types.ArchiveExtractionUploading, types.ArchiveExtractionParsing, types.ArchiveExtractionExtracting, types.ArchiveExtractionLinking:
			return nil, ErrArchiveInvalidState
		}
		now := time.Now()
		row.ArchivedAt = &now
	} else {
		wasTrashed := row.TrashedAt != nil
		if wasTrashed {
			if err := s.restoreManagedKnowledgeMirror(ctx, tenantID, row); err != nil {
				return nil, err
			}
			row.TrashedAt = nil
		}
		row.ArchivedAt = nil
	}
	if err := s.repo.UpdateDocument(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// clearDocumentReminderAssociations removes delivery artifacts when their
// source document leaves the active archive. Reminders remain as canceled
// records for audit, while their notifications and one-shot delivery state no
// longer have a useful source to point at.
func (s *smartArchiveService) clearDocumentReminderAssociations(ctx context.Context, tenantID uint64, documentID string) error {
	reminders, err := s.repo.ListReminders(ctx, tenantID, "")
	if err != nil {
		return err
	}
	var firstErr error
	for _, reminder := range reminders {
		if reminder == nil || reminder.DocumentID != documentID {
			continue
		}
		if reminder.Status != types.ArchiveReminderHandled && reminder.Status != types.ArchiveReminderCanceled {
			reminder.Status = types.ArchiveReminderCanceled
			if updateErr := s.repo.UpdateReminder(ctx, reminder); updateErr != nil && firstErr == nil {
				firstErr = updateErr
			}
		}
		if cleanupErr := s.repo.DeleteReminderDeliveryArtifacts(ctx, tenantID, reminder.ID); cleanupErr != nil && firstErr == nil {
			firstErr = cleanupErr
		}
	}
	if candidates, listErr := s.repo.ListReminderCandidates(ctx, tenantID, ""); listErr != nil {
		if firstErr == nil {
			firstErr = listErr
		}
	} else {
		for _, candidate := range candidates {
			if candidate == nil || candidate.DocumentID != documentID || candidate.Status != types.ArchiveReminderCandidatePending {
				continue
			}
			candidate.Status = types.ArchiveReminderCandidateSuperseded
			if updateErr := s.repo.UpdateReminderCandidate(ctx, candidate); updateErr != nil && firstErr == nil {
				firstErr = updateErr
			}
		}
	}
	return firstErr
}

func (s *smartArchiveService) DeleteDocument(ctx context.Context, tenantID uint64, id string) error {
	row, err := s.GetDocument(ctx, tenantID, id)
	if err != nil {
		return err
	}
	wasTrashed := row.TrashedAt != nil
	if !wasTrashed {
		now := time.Now()
		row.TrashedAt = &now
		if err := s.repo.UpdateDocument(ctx, row); err != nil {
			return err
		}
	}
	if err := s.deleteManagedKnowledgeMirror(ctx, tenantID, row); err != nil {
		// If the cascade cannot remove the managed mirror, roll back the
		// newly-created trash marker so the user is not left with an apparently
		// deleted archive whose searchable copy is still active.
		if !wasTrashed {
			row.TrashedAt = nil
			_ = s.repo.UpdateDocument(ctx, row)
		}
		return err
	}
	if err := s.repo.UpdateDocument(ctx, row); err != nil {
		return err
	}
	// Moving the source document to trash is the user-facing operation. Keep
	// reminder cleanup best-effort here: an older deployment may be missing a
	// reminder migration, and an auxiliary cleanup failure must not turn a
	// successful archive deletion into a 500 after the trash marker is already
	// persisted. cleanupExpiredTrash retries the same cleanup before a row is
	// permanently removed.
	if err := s.clearDocumentReminderAssociations(ctx, tenantID, id); err != nil {
		logger.Warnf(ctx, "smart archive: reminder cleanup deferred for trashed document %s: %v", id, err)
	}
	return nil
}

// permanentlyDeleteDocument is intentionally separate from DeleteDocument:
// the latter is the user-facing recycle-bin operation, while this path is
// reserved for the explicit admin-only bulk purge action.
func (s *smartArchiveService) permanentlyDeleteDocument(ctx context.Context, tenantID uint64, id string) error {
	row, err := s.GetDocument(ctx, tenantID, id)
	if err != nil {
		return err
	}
	// Purge is exposed from the archived view, so keep the same state
	// restriction in the service layer as well. This prevents a caller from
	// turning the endpoint into a general-purpose destructive delete API.
	if row.ArchivedAt == nil || row.TrashedAt != nil {
		return ErrArchiveInvalidState
	}

	// Reminders and candidates do not have a document foreign key with
	// cascading deletion. Stop their future work and remove their delivery
	// artifacts before removing the source document.
	if err := s.clearDocumentReminderAssociations(ctx, tenantID, id); err != nil {
		return err
	}

	if strings.TrimSpace(row.KnowledgeID) != "" && s.knowledge == nil {
		return errors.New("knowledge service is unavailable for permanent archive deletion")
	}
	if err := s.deleteManagedKnowledgeMirror(ctx, tenantID, row); err != nil {
		return err
	}
	if row.FilePath != "" {
		if err := s.deleteArchiveSourceFile(ctx, row); err != nil {
			return err
		}
	}
	if s.parseArtifacts != nil {
		if err := s.parseArtifacts.DeleteBySourceDocument(archiveContextWithTenant(ctx, tenantID), tenantID, id); err != nil {
			return err
		}
	}
	return s.repo.HardDeleteDocument(ctx, tenantID, id)
}

// deleteArchiveSourceFile releases the archive owner before deleting the
// physical object. Shared resource references remain alive until their last
// owner releases them; legacy bare paths retain the historical delete path.
func (s *smartArchiveService) deleteArchiveSourceFile(ctx context.Context, doc *types.ArchiveDocument) error {
	if doc == nil || strings.TrimSpace(doc.FilePath) == "" {
		return nil
	}
	if s.files == nil {
		return errors.New("file service is unavailable for permanent archive deletion")
	}
	if s.resources != nil {
		remaining, err := s.resources.Release(ctx, doc.FilePath, types.ResourceOwnerSmartArchive, doc.ID)
		if err != nil {
			return err
		}
		if remaining > 0 {
			return nil
		}
	}
	return s.files.DeleteFile(ctx, doc.FilePath)
}

func (s *smartArchiveService) BatchDocumentAction(ctx context.Context, tenantID uint64, ids []string, action types.ArchiveBulkAction) (*types.ArchiveBulkActionResult, error) {
	switch action {
	case types.ArchiveBulkArchive, types.ArchiveBulkRestore, types.ArchiveBulkDelete, types.ArchiveBulkPurge:
	default:
		return nil, ErrArchiveInvalidState
	}
	if len(ids) == 0 || len(ids) > 500 {
		return nil, ErrArchiveInvalidState
	}
	result := &types.ArchiveBulkActionResult{Action: action, Items: make([]types.ArchiveBulkActionItem, 0, len(ids))}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result.Requested++
		item := types.ArchiveBulkActionItem{ID: id}
		var err error
		switch action {
		case types.ArchiveBulkArchive:
			_, err = s.ArchiveDocument(ctx, tenantID, id, true)
		case types.ArchiveBulkRestore:
			_, err = s.ArchiveDocument(ctx, tenantID, id, false)
		case types.ArchiveBulkDelete:
			err = s.DeleteDocument(ctx, tenantID, id)
		case types.ArchiveBulkPurge:
			err = s.permanentlyDeleteDocument(ctx, tenantID, id)
		}
		if err != nil {
			item.Error = err.Error()
			result.Failed++
		} else {
			item.Success = true
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	if result.Requested == 0 {
		return nil, ErrArchiveInvalidState
	}
	return result, nil
}
func (s *smartArchiveService) Search(ctx context.Context, tenantID uint64, req *types.ArchiveSearchRequest) (*types.ArchiveSearchResponse, error) {
	if req == nil {
		return s.repo.Search(ctx, tenantID, req)
	}
	// Keep the public request natural-language friendly while handing the
	// repository deterministic SQL filters for the common archive queries.
	normalized := *req
	normalized.Filters = req.Filters
	lower := strings.ToLower(req.Query)
	if normalized.Filters.SerialNumber == "" {
		if match := regexp.MustCompile(`(?i)\b(SN[\s:#-]?[A-Za-z0-9_-]+)`).FindStringSubmatch(req.Query); len(match) == 2 {
			normalized.Filters.SerialNumber = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(match[1], " ", ""), ":", ""), "#", "")
		}
	}
	if normalized.Filters.Model == "" {
		if match := regexp.MustCompile(`(?i)\b([A-Z][A-Z0-9-]{2,})\b`).FindStringSubmatch(req.Query); len(match) == 2 && !strings.HasPrefix(strings.ToUpper(match[1]), "SN") {
			normalized.Filters.Model = match[1]
		}
	}
	if normalized.Filters.From == nil && normalized.Filters.To == nil && strings.Contains(lower, "去年") {
		now := time.Now()
		from := time.Date(now.Year()-1, time.January, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(now.Year()-1, time.December, 31, 23, 59, 59, 0, time.UTC)
		normalized.Filters.From, normalized.Filters.To = &from, &to
	}
	return s.repo.Search(ctx, tenantID, &normalized)
}
func (s *smartArchiveService) ListReminders(ctx context.Context, tenantID uint64, status string) ([]*types.ArchiveReminder, error) {
	return s.repo.ListReminders(ctx, tenantID, status)
}

func normalizeArchiveBulkIDs(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, ErrArchiveInvalidState
	}
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	if len(result) == 0 {
		return nil, ErrArchiveInvalidState
	}
	return result, nil
}

func (s *smartArchiveService) BatchDeleteReminders(ctx context.Context, tenantID uint64, ids []string) (*types.ArchiveBulkActionResult, error) {
	ids, err := normalizeArchiveBulkIDs(ids)
	if err != nil {
		return nil, err
	}
	result := &types.ArchiveBulkActionResult{Action: types.ArchiveBulkDelete, Items: make([]types.ArchiveBulkActionItem, 0, len(ids))}
	for _, id := range ids {
		result.Requested++
		item := types.ArchiveBulkActionItem{ID: id}
		if err := s.repo.DeleteReminder(ctx, tenantID, id); err != nil {
			item.Error = err.Error()
			result.Failed++
		} else {
			item.Success = true
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	if result.Succeeded > 0 {
		// The deleted rows are no longer a scheduler source. One wake-up after
		// the batch is enough to recalculate the durable next wake time.
		s.signalReminderScheduleChanged()
	}
	return result, nil
}

func (s *smartArchiveService) ListReminderCandidates(ctx context.Context, tenantID uint64, status string) ([]*types.ArchiveReminderCandidate, error) {
	if status == "" {
		status = string(types.ArchiveReminderCandidatePending)
	}
	switch types.ArchiveReminderCandidateStatus(status) {
	case types.ArchiveReminderCandidatePending, types.ArchiveReminderCandidateCreated, types.ArchiveReminderCandidateSuperseded, types.ArchiveReminderCandidateIgnored:
	default:
		return nil, ErrArchiveInvalidState
	}
	return s.repo.ListReminderCandidates(ctx, tenantID, status)
}

func (s *smartArchiveService) BatchIgnoreReminderCandidates(ctx context.Context, tenantID uint64, ids []string) (*types.ArchiveBulkActionResult, error) {
	ids, err := normalizeArchiveBulkIDs(ids)
	if err != nil {
		return nil, err
	}
	result := &types.ArchiveBulkActionResult{Action: types.ArchiveBulkIgnore, Items: make([]types.ArchiveBulkActionItem, 0, len(ids))}
	for _, id := range ids {
		result.Requested++
		item := types.ArchiveBulkActionItem{ID: id}
		candidate, getErr := s.repo.GetReminderCandidate(ctx, tenantID, id)
		switch {
		case errors.Is(getErr, gorm.ErrRecordNotFound):
			item.Error = ErrArchiveNotFound.Error()
		case getErr != nil:
			item.Error = getErr.Error()
		case candidate.Status != types.ArchiveReminderCandidatePending:
			item.Error = ErrArchiveInvalidState.Error()
		default:
			ignoreErr := s.repo.IgnoreReminderCandidate(ctx, tenantID, id)
			if errors.Is(ignoreErr, gorm.ErrRecordNotFound) {
				item.Error = ErrArchiveInvalidState.Error()
			} else if ignoreErr != nil {
				item.Error = ignoreErr.Error()
			} else {
				item.Success = true
				result.Succeeded++
			}
		}
		if !item.Success {
			result.Failed++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *smartArchiveService) CreateReminderFromCandidate(ctx context.Context, tenantID uint64, userID, id string, offsetDays int, eventTime, assigneeID string) (*types.ArchiveReminder, error) {
	candidate, err := s.repo.GetReminderCandidate(ctx, tenantID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrArchiveNotFound
	}
	if err != nil {
		return nil, err
	}
	if candidate.Status == types.ArchiveReminderCandidateCreated && candidate.ReminderID != "" {
		return s.repo.GetReminder(ctx, tenantID, candidate.ReminderID)
	}
	if candidate.Status != types.ArchiveReminderCandidatePending || candidate.NeedsReview {
		return nil, ErrArchiveInvalidState
	}
	if offsetDays < 0 || offsetDays > 3650 {
		return nil, ErrArchiveInvalidState
	}
	settings, err := s.GetSettings(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		loc = time.FixedZone("archive", 8*3600)
	}
	parsedTime := "09:00"
	if strings.TrimSpace(eventTime) != "" {
		parsedTime = strings.TrimSpace(eventTime)
	}
	hour, minute := 9, 0
	if _, scanErr := fmt.Sscanf(parsedTime, "%d:%d", &hour, &minute); scanErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return nil, ErrArchiveInvalidState
	}
	due := time.Date(candidate.EventAt.Year(), candidate.EventAt.Month(), candidate.EventAt.Day(), hour, minute, 0, 0, loc).AddDate(0, 0, -offsetDays)
	rule := map[string]any{}
	_ = json.Unmarshal(candidate.Rule, &rule)
	rule["offset_days"] = offsetDays
	rule["time"] = fmt.Sprintf("%02d:%02d", hour, minute)
	rule["timezone"] = settings.Timezone
	ruleJSON, _ := json.Marshal(rule)
	if strings.TrimSpace(assigneeID) == "" {
		assigneeID = candidate.AssigneeID
	}
	if strings.TrimSpace(assigneeID) == "" {
		assigneeID = userID
	}
	if s.members != nil {
		membership, membershipErr := s.members.GetMembership(ctx, assigneeID, tenantID)
		if membershipErr != nil || membership == nil || membership.Status != types.TenantMemberStatusActive {
			return nil, ErrArchivePermission
		}
	}
	dueUTC := due.UTC()
	reminder := &types.ArchiveReminder{TenantID: tenantID, DocumentID: candidate.DocumentID, CustomerID: candidate.CustomerID, AssigneeID: assigneeID, Type: candidate.Type, Title: candidate.Title, Description: candidate.Description, Rule: types.JSON(ruleJSON), Status: types.ArchiveReminderDraft, Confidence: candidate.Confidence, DueAt: &dueUTC, CreatedBy: userID}
	if err := s.repo.CreateReminderFromCandidate(ctx, candidate, reminder); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			// Another request may have won the transaction between the initial
			// read and insert. Reload the candidate so repeated clicks return the
			// same formal reminder instead of surfacing a 500.
			if current, reloadErr := s.repo.GetReminderCandidate(ctx, tenantID, id); reloadErr == nil && current.ReminderID != "" {
				return s.repo.GetReminder(ctx, tenantID, current.ReminderID)
			}
		}
		return nil, err
	}
	s.signalReminderScheduleChanged()
	return reminder, nil
}

func (s *smartArchiveService) BackfillReminderCandidates(ctx context.Context) error {
	docs, err := s.repo.ListCompletedDocuments(ctx)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if doc == nil || !s.legalWorkspaceEnabled(ctx, doc.TenantID) {
			continue
		}
		fields := map[string]string{}
		_ = json.Unmarshal(doc.ExtractedFields, &fields)
		evidence, evidenceErr := s.repo.ListEvidence(ctx, doc.TenantID, doc.ID)
		if evidenceErr != nil {
			continue
		}
		// Older completed image imports could contain perfectly usable OCR text
		// but miss a field because the source used Markdown emphasis (for
		// example **2025 年 11 月 1 日**). Re-run the deterministic field parser
		// during the one-time/startup backfill so a parser fix repairs those rows
		// without sending the image to the VLM again.
		if strings.TrimSpace(doc.ExtractedText) != "" {
			manualFields := make(map[string]bool)
			markdownEvidence := false
			for _, row := range evidence {
				if row == nil {
					continue
				}
				if row.IsManual {
					manualFields[row.FieldName] = true
				}
				if strings.Contains(row.Value, "**") || strings.Contains(row.Quote, "**") {
					markdownEvidence = true
				}
			}
			parseText := doc.ExtractedText
			textChanged := false
			if archiveImageExtension(doc.FileType) {
				parseText = normalizeArchiveOCRText(parseText)
				textChanged = parseText != doc.ExtractedText
			}
			previousType, previousBusiness := doc.DocumentType, doc.BusinessType
			parsed, parsedEvidence := extractArchiveFields(doc, parseText)
			changed := previousType != doc.DocumentType || previousBusiness != doc.BusinessType || textChanged
			for _, field := range []string{"customer", "borrower", "lender"} {
				if manualFields[field] {
					continue
				}
				current := fields[field]
				cleaned := cleanArchiveEntityName(current)
				if cleaned == current {
					continue
				}
				changed = true
				if cleaned == "" {
					delete(fields, field)
					if field == "customer" {
						doc.CustomerID = ""
					}
				} else {
					fields[field] = cleaned
				}
			}
			if textChanged {
				doc.ExtractedText = parseText
			}
			if archiveImageExtension(doc.FileType) {
				for key, value := range fields {
					if manualFields[key] {
						continue
					}
					cleaned := normalizeArchiveOCRText(value)
					if cleaned != value {
						fields[key] = cleaned
						changed = true
					}
				}
			}
			replacedEvidenceFields := make(map[string]bool)
			for key, value := range parsed {
				if manualFields[key] || strings.TrimSpace(value) == "" {
					continue
				}
				if strings.TrimSpace(fields[key]) == "" || strings.Contains(fields[key], "**") || fields[key] != value {
					replacedEvidenceFields[key] = true
					fields[key] = value
					changed = true
				}
			}
			if len(parsedEvidence) > 0 {
				if archiveImageExtension(doc.FileType) && (textChanged || markdownEvidence) {
					// Rebuild AI evidence against the normalized OCR text so the
					// quote shown in the UI and its character range stay aligned.
					cleanEvidence := make([]*types.ArchiveFieldEvidence, 0, len(parsedEvidence))
					for _, row := range evidence {
						if row != nil && row.IsManual {
							cleanEvidence = append(cleanEvidence, row)
						}
					}
					cleanEvidence = append(cleanEvidence, parsedEvidence...)
					evidence = cleanEvidence
					changed = true
				} else {
					if len(replacedEvidenceFields) > 0 || len(parsed) > 0 {
						cleanEvidence := evidence[:0]
						for _, row := range evidence {
							if row == nil {
								cleanEvidence = append(cleanEvidence, row)
								continue
							}
							expected := parsed[row.FieldName]
							staleParsedValue := !row.IsManual && expected != "" && cleanArchiveEntityName(row.Value) != cleanArchiveEntityName(expected)
							if row.IsManual || (!replacedEvidenceFields[row.FieldName] && !staleParsedValue) {
								cleanEvidence = append(cleanEvidence, row)
							} else {
								changed = true
							}
						}
						evidence = cleanEvidence
					}
					seenEvidence := make(map[string]bool, len(evidence))
					for _, row := range evidence {
						if row != nil {
							seenEvidence[row.FieldName+"\x00"+row.Value+"\x00"+row.Quote] = true
						}
					}
					for _, row := range parsedEvidence {
						if row == nil {
							continue
						}
						key := row.FieldName + "\x00" + row.Value + "\x00" + row.Quote
						if seenEvidence[key] {
							continue
						}
						evidence = append(evidence, row)
						seenEvidence[key] = true
						changed = true
					}
				}
			}
			previousCustomerID := doc.CustomerID
			if linkErr := s.linkCustomer(ctx, doc, fields); linkErr != nil {
				return linkErr
			}
			if doc.CustomerID != previousCustomerID {
				changed = true
			}
			if changed {
				if encoded, marshalErr := json.Marshal(fields); marshalErr == nil {
					doc.ExtractedFields = types.JSON(encoded)
					if doc.ExtractionStatus == types.ArchiveExtractionReview && len(evidence) > 0 {
						doc.ExtractionStatus = types.ArchiveExtractionCompleted
					}
					if updateErr := s.repo.UpdateDocument(ctx, doc); updateErr != nil {
						return updateErr
					}
					if evidenceErr := s.repo.ReplaceEvidence(ctx, doc.TenantID, doc.ID, evidence); evidenceErr != nil {
						return evidenceErr
					}
				}
			}
		}
		if err := s.detectReminderCandidates(ctx, doc, fields, evidence, doc.CreatedBy); err != nil {
			return err
		}
	}
	return nil
}
func (s *smartArchiveService) CreateReminder(ctx context.Context, tenantID uint64, userID string, row *types.ArchiveReminder) (*types.ArchiveReminder, error) {
	row.TenantID = tenantID
	if row.AssigneeID == "" {
		row.AssigneeID = userID
	}
	if s.members != nil {
		membership, membershipErr := s.members.GetMembership(ctx, row.AssigneeID, tenantID)
		if membershipErr != nil || membership == nil || membership.Status != types.TenantMemberStatusActive {
			return nil, ErrArchivePermission
		}
	}
	row.CreatedBy = userID
	// Formal reminders are always created as an explicit, non-scheduled draft;
	// activation is a separate user action and the only path into the scanner.
	row.Status = types.ArchiveReminderDraft
	if err := s.repo.CreateReminder(ctx, row); err != nil {
		return nil, err
	}
	s.signalReminderScheduleChanged()
	return row, nil
}
func (s *smartArchiveService) UpdateReminderStatus(ctx context.Context, tenantID uint64, id string, status types.ArchiveReminderStatus, snooze *time.Time, assigneeID string) (*types.ArchiveReminder, error) {
	row, err := s.repo.GetReminder(ctx, tenantID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrArchiveNotFound
	}
	if err != nil {
		return nil, err
	}
	switch status {
	case types.ArchiveReminderDraft, types.ArchiveReminderActive, types.ArchiveReminderSnoozed, types.ArchiveReminderHandled, types.ArchiveReminderCanceled:
	default:
		return nil, ErrArchiveInvalidState
	}
	row.Status = status
	row.SnoozedUntil = snooze
	if strings.TrimSpace(assigneeID) != "" {
		row.AssigneeID = strings.TrimSpace(assigneeID)
	}
	if err := s.repo.UpdateReminder(ctx, row); err != nil {
		return nil, err
	}
	s.signalReminderScheduleChanged()
	return row, nil
}
func (s *smartArchiveService) DeleteReminder(ctx context.Context, tenantID uint64, id string) error {
	if err := s.repo.DeleteReminder(ctx, tenantID, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrArchiveNotFound
		}
		return err
	}
	s.signalReminderScheduleChanged()
	return nil
}
func (s *smartArchiveService) ListNotifications(ctx context.Context, tenantID uint64, userID string, unread bool) ([]*types.ArchiveNotification, error) {
	return s.repo.ListNotifications(ctx, tenantID, userID, unread)
}
func (s *smartArchiveService) MarkNotificationRead(ctx context.Context, tenantID uint64, userID, id string) error {
	return s.repo.MarkNotificationRead(ctx, tenantID, userID, id)
}
func (s *smartArchiveService) DeleteNotification(ctx context.Context, tenantID uint64, userID, id string) error {
	if err := s.repo.DeleteNotification(ctx, tenantID, userID, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrArchiveNotFound
		}
		return err
	}
	return nil
}

func (s *smartArchiveService) NextReminderWakeAt(ctx context.Context) (*time.Time, error) {
	return s.repo.NextReminderWakeAt(ctx)
}

func (s *smartArchiveService) RunDueReminders(ctx context.Context) error {
	rows, err := s.repo.ListDueReminders(ctx, 200)
	if err != nil {
		return err
	}
	var firstErr error
	for _, row := range rows {
		if row == nil || row.DueAt == nil {
			continue
		}
		if !s.legalWorkspaceEnabled(ctx, row.TenantID) {
			continue
		}
		if row.Status == types.ArchiveReminderSnoozed {
			row.Status = types.ArchiveReminderActive
			row.SnoozedUntil = nil
			if updateErr := s.repo.UpdateReminder(ctx, row); updateErr != nil {
				if firstErr == nil {
					firstErr = updateErr
				}
				continue
			}
		}
		fingerprint := row.ID + ":" + row.DueAt.UTC().Format(time.RFC3339)
		occurrence := &types.ArchiveReminderOccurrence{ReminderID: row.ID, TenantID: row.TenantID, Fingerprint: fingerprint, DueAt: *row.DueAt, Status: types.ArchiveOccurrencePending}
		notification := &types.ArchiveNotification{TenantID: row.TenantID, UserID: row.AssigneeID, ReminderID: row.ID, Title: row.Title, Body: row.Description}
		if row.Type == types.ArchiveReminderMissingReturn {
			doc, docErr := s.GetDocument(ctx, row.TenantID, row.DocumentID)
			if docErr == nil && hasArchiveReturnRecord(ctx, s.repo, doc) {
				occurrence.Status = types.ArchiveOccurrenceSkipped
			}
		}
		if deliveryErr := s.repo.DeliverReminder(ctx, row, occurrence, notification); deliveryErr != nil {
			if firstErr == nil {
				firstErr = deliveryErr
			}
		}
	}
	if cleanupErr := s.cleanupExpiredTrash(ctx); cleanupErr != nil && firstErr == nil {
		firstErr = cleanupErr
	}
	return firstErr
}

// legalWorkspaceEnabled keeps reminder delivery fail-closed for tenants that
// have disabled the shared legal shell. A nil tenant repository is retained as
// an in-memory/test fallback; production containers always provide it.
func (s *smartArchiveService) legalWorkspaceEnabled(ctx context.Context, tenantID uint64) bool {
	if s.tenantRepo == nil {
		return true
	}
	tenant, err := s.tenantRepo.GetTenantByID(ctx, tenantID)
	return err == nil && tenant != nil && tenant.LegalWorkspaceConfig.IsEnabled()
}

func hasArchiveReturnRecord(ctx context.Context, repo interfaces.ArchiveRepository, loan *types.ArchiveDocument) bool {
	if loan == nil {
		return false
	}
	if loan.ReturnedAt != nil || strings.TrimSpace(loan.AgreementNumber) == "" {
		return loan.ReturnedAt != nil
	}
	rows, err := repo.ListDocuments(ctx, loan.TenantID, loan.AgreementNumber, false)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row == nil || row.ID == loan.ID {
			continue
		}
		if row.DocumentType == types.ArchiveDocumentReturn || row.ReturnedAt != nil {
			return true
		}
	}
	return false
}

func (s *smartArchiveService) cleanupExpiredTrash(ctx context.Context) error {
	rows, err := s.repo.ListTrashedDocuments(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, row := range rows {
		if row == nil || row.TrashedAt == nil {
			continue
		}
		retention := 30
		if settings, settingsErr := s.repo.GetSettings(ctx, row.TenantID); settingsErr == nil && settings.TrashRetentionDays > 0 {
			retention = settings.TrashRetentionDays
		}
		if row.TrashedAt.AddDate(0, 0, retention).After(now) {
			continue
		}
		if err := s.clearDocumentReminderAssociations(ctx, row.TenantID, row.ID); err != nil {
			logger.Warnf(ctx, "smart archive: deferred reminder cleanup skipped for %s: %v", row.ID, err)
			continue
		}
		// Older releases could move an archive row to trash without deleting
		// its managed Knowledge Base mirror. Clean that mirror before removing
		// the source row; if identity validation or cleanup fails, retain the
		// trash row for a later retry instead of leaving searchable stale data.
		if row.KnowledgeID != "" {
			if err := s.deleteManagedKnowledgeMirror(ctx, row.TenantID, row); err != nil {
				logger.Warnf(ctx, "smart archive: deferred mirror cleanup skipped for %s: %v", row.ID, err)
				continue
			}
		}
		if row.FilePath != "" {
			if err := s.deleteArchiveSourceFile(ctx, row); err != nil {
				logger.Warnf(ctx, "smart archive: source cleanup failed for %s: %v", row.ID, err)
				continue
			}
		}
		if s.parseArtifacts != nil {
			if err := s.parseArtifacts.DeleteBySourceDocument(archiveContextWithTenant(ctx, row.TenantID), row.TenantID, row.ID); err != nil {
				logger.Warnf(ctx, "smart archive: parse artifact cleanup failed for %s: %v", row.ID, err)
			}
		}
		_ = s.repo.HardDeleteDocument(ctx, row.TenantID, row.ID)
	}
	return nil
}

var archiveDateRE = regexp.MustCompile(`(?i)(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})`)
var archiveChineseDateRE = regexp.MustCompile(`(?i)(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日?`)

func parseArchiveDate(value string) (time.Time, error) {
	match := archiveDateRE.FindStringSubmatch(value)
	if len(match) != 4 {
		match = archiveChineseDateRE.FindStringSubmatch(value)
	}
	if len(match) != 4 {
		return time.Time{}, errors.New("date not found")
	}
	y, _ := strconv.Atoi(match[1])
	m, _ := strconv.Atoi(match[2])
	d, _ := strconv.Atoi(match[3])
	if y < 1 || m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, errors.New("invalid date")
	}
	parsed := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if parsed.Year() != y || int(parsed.Month()) != m || parsed.Day() != d {
		return time.Time{}, errors.New("invalid date")
	}
	return parsed, nil
}

// parseArchiveFieldDate keeps the generic parser's first-match behavior for
// signed/effective dates, but uses the final date for deadline fields such as
// “劳动合同期限：2024-01-01 至 2025-01-01”. A deadline must never become the
// start date merely because both dates occur in the same source line.
func parseArchiveFieldDate(field, value string) (time.Time, error) {
	if field != "expires_at" && field != "return_due_at" && field != "renewed_at" {
		return parseArchiveDate(value)
	}
	matches := archiveDateRE.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		matches = archiveChineseDateRE.FindAllStringSubmatch(value, -1)
	}
	if len(matches) == 0 {
		return time.Time{}, errors.New("date not found")
	}
	last := matches[len(matches)-1]
	return parseArchiveDate(strings.Join(last[1:], "-"))
}

type archiveFieldDefinition struct {
	name, pattern string
	confidence    float64
}

type archiveDocumentTypeMarker struct {
	documentType types.ArchiveDocumentType
	markers      []string
}

// archiveDocumentTypeMarkers deliberately contains document-level phrases.
// Clause words such as "付款方式" or "归还日期" are handled as weak signals
// below, because they frequently occur inside an otherwise ordinary contract.
var archiveDocumentTypeMarkers = []archiveDocumentTypeMarker{
	{types.ArchiveDocumentLoanAgreement, []string{"借用协议", "借用合同", "借用单", "设备借用", "loan agreement", "loan contract", "borrowing agreement"}},
	{types.ArchiveDocumentOutbound, []string{"出库单", "出库记录", "出货单", "发货单", "outbound order", "shipping order"}},
	{types.ArchiveDocumentReturn, []string{"归还单", "归还记录", "退库单", "返还单", "return order", "return receipt", "return record"}},
	{types.ArchiveDocumentRenewal, []string{"续约协议", "续签协议", "续约文件", "renewal agreement", "renewal contract"}},
	{types.ArchiveDocumentPayment, []string{"付款记录", "付款凭证", "付款单", "付款申请", "付款通知", "支付凭证", "payment record", "payment voucher", "payment receipt", "payment request", "invoice"}},
	{types.ArchiveDocumentDelivery, []string{"交付单", "交付记录", "交货单", "签收单", "收货单", "delivery note", "delivery record", "delivery receipt", "proof of delivery"}},
	{types.ArchiveDocumentContract, []string{"合同", "协议", "contract", "agreement"}},
}

// archiveDocumentHeader returns the small leading portion where scanned and
// exported documents normally put their title/type. Keeping this separate
// from the full body prevents a clause buried in a contract from outweighing
// the document title.
func archiveDocumentHeader(content string) string {
	lines := strings.Split(content, "\n")
	var header []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		header = append(header, line)
		if len(header) >= 12 {
			break
		}
	}
	return strings.ToLower(strings.Join(header, " "))
}

func addArchiveTypeScores(scores map[types.ArchiveDocumentType]int, text string, weight int, markers []archiveDocumentTypeMarker) {
	if text == "" {
		return
	}
	for _, marker := range markers {
		for _, value := range marker.markers {
			if strings.Contains(text, strings.ToLower(value)) {
				scores[marker.documentType] += weight
				break
			}
		}
	}
}

func classifyArchiveDocument(doc *types.ArchiveDocument, content string) {
	if doc == nil {
		return
	}
	filename := strings.ToLower(strings.TrimSpace(doc.FileName))
	header := archiveDocumentHeader(content)
	body := strings.ToLower(content)
	scores := make(map[types.ArchiveDocumentType]int)
	addArchiveTypeScores(scores, filename, 100, archiveDocumentTypeMarkers)
	addArchiveTypeScores(scores, header, 80, archiveDocumentTypeMarkers)
	addArchiveTypeScores(scores, body, 20, archiveDocumentTypeMarkers)

	// These terms identify a clause or field, not necessarily a document type.
	// They are useful only when no stronger title/filename signal exists.
	weakMarkers := []archiveDocumentTypeMarker{
		{types.ArchiveDocumentLoanAgreement, []string{"借用", "loan", "borrow"}},
		{types.ArchiveDocumentOutbound, []string{"出库", "outbound"}},
		{types.ArchiveDocumentReturn, []string{"归还", "退库", "return"}},
		{types.ArchiveDocumentRenewal, []string{"续约", "续签", "renewal"}},
		{types.ArchiveDocumentPayment, []string{"付款", "支付", "payment"}},
		{types.ArchiveDocumentDelivery, []string{"交付", "交货", "签收", "delivery"}},
		{types.ArchiveDocumentContract, []string{"合同", "contract"}},
	}
	addArchiveTypeScores(scores, body, 5, weakMarkers)

	// Specific document titles must win over the generic “协议/合同” marker.
	// In particular, “借用协议” contains both markers and must remain a loan
	// agreement so its return deadline can produce reminder candidates.
	precedence := []types.ArchiveDocumentType{
		types.ArchiveDocumentLoanAgreement,
		types.ArchiveDocumentOutbound,
		types.ArchiveDocumentReturn,
		types.ArchiveDocumentRenewal,
		types.ArchiveDocumentPayment,
		types.ArchiveDocumentDelivery,
		types.ArchiveDocumentContract,
	}
	bestType := types.ArchiveDocumentOther
	bestScore := 0
	for _, candidate := range precedence {
		if scores[candidate] > bestScore {
			bestType, bestScore = candidate, scores[candidate]
		}
	}
	if bestScore == 0 {
		doc.DocumentType = types.ArchiveDocumentOther
		return
	}
	doc.DocumentType = bestType
	if bestType == types.ArchiveDocumentLoanAgreement || bestType == types.ArchiveDocumentOutbound {
		doc.BusinessType = types.ArchiveBusinessLoan
	} else if bestType == types.ArchiveDocumentContract {
		doc.BusinessType = types.ArchiveBusinessSale
	}
}

var archiveFieldPatterns = []archiveFieldDefinition{
	{"customer", `(?im)(?:客户|Customer|签收单位(?:名称)?|购买方|采购方|采购人|甲方[ \t]*[（(](?:买方|采购人)[）)])[ \t]*[:：][ \t]*([^\n,，;；]+)`, 0.91},
	{"borrower", `(?im)(?:乙方(?:[ \t]*[（(][^\n）)]*[）)])?|借用方|承租方)[ \t]*[:：][ \t]*([^\n,，;；]+)`, 0.94},
	{"lender", `(?im)(?:甲方(?:[ \t]*[（(][^\n）)]*[）)])?|出借方|出租方)[ \t]*[:：][ \t]*([^\n,，;；]+)`, 0.90},
	{"agreement_number", `(?im)(?:协议编号|合同编号|Agreement(?:\s+No\.?)?|Contract(?:\s+No\.?)?)\s*[:：#]?\s*([A-Za-z0-9][A-Za-z0-9_\-/]*)`, 0.94},
	{"expires_at", `(?im)(?:合同到期日期|劳动合同到期日|合同终止日|合同期限至|服务期限至|到期日期|到期日|合同到期|终止日期|有效期至|租期至|期限至|Expiration\s+Date|Expiration|Expiry(?:\s+Date)?)\s*[:：]?\s*([^\n,，;；]+)`, 0.88},
	{"return_due_at", `(?im)(?:借用期限|借用期|租期|使用期限|设备借用期限)[^。\n]{0,100}?(?:至|到|截止(?:日期)?|止)\s*(?:\*{1,2}\s*)?([0-9]{4}\s*年\s*[0-9]{1,2}\s*月\s*[0-9]{1,2}\s*日?|[0-9]{4}[-/.][0-9]{1,2}[-/.][0-9]{1,2})`, 0.91},
	{"return_due_at", `(?im)^\s*(?:应归还日期|应归还日|预计归还日期|预计归还日|计划归还日期|计划归还日|设备归还日期|设备归还日|借用到期日|归还期限|归还日期|归还日|Return\s+Date)\s*[:：]?\s*([^\n,，;；]+)`, 0.88},
	{"signed_at", `(?im)(?:签署日期|签订日期|Signed\s+Date)\s*[:：]?\s*([^\n,，;；]+)`, 0.86},
	{"effective_at", `(?im)(?:生效日期|Effective\s+Date)\s*[:：]?\s*([^\n,，;；]+)`, 0.86},
	{"returned_at", `(?im)^\s*(?:实际归还日期|实际归还日|归还完成日期|归还完成日|Returned\s+Date)\s*[:：]?\s*([^\n,，;；]+)`, 0.88},
	{"renewed_at", `(?im)(?:续约日期|Renewal\s+Date)\s*[:：]?\s*([^\n,，;；]+)`, 0.85},
	{"payment_at", `(?im)(?:付款日期|付款日|Payment\s+Date)\s*[:：]?\s*([^\n,，;；]+)`, 0.86},
	{"delivery_at", `(?im)(?:交付日期|交付日|出库日期|出库日|发货日期|签收日期|Delivery\s+Date)\s*[:：]?\s*([^\n,，;；]+)`, 0.86},
	{"asset_name", `(?im)(?:物料名称|设备名称|产品名称|资产名称)\s*[:：]?\s*([^\n|，,;；]+)`, 0.84},
	{"asset_model", `(?im)(?:规格型号|物料参数|设备型号|产品型号|型号)\s*[:：]?\s*([^\n|，,;；]+)`, 0.84},
	{"serial_number", `(?im)(?:序列号|SN|设备编号|物料编码|资产编号)\s*[:：]?\s*([A-Za-z0-9][A-Za-z0-9_\-]*)`, 0.86},
	{"quantity", `(?im)(?:数量|数\s*量)\s*[:：]?\s*(\d+(?:\.\d+)?)`, 0.84},
	{"amount", `(?im)(?:金额|总价|Amount|Total)\s*[:：]?\s*([A-Z]{0,3}\s*[\d,]+(?:\.\d{1,2})?)`, 0.82},
}

func archiveEvidenceLocatorKind(doc *types.ArchiveDocument) types.ArchiveSourceLocatorKind {
	if doc == nil {
		return types.ArchiveLocatorText
	}
	switch doc.FileType {
	case ".pdf":
		return types.ArchiveLocatorPDFPage
	case ".docx", ".doc":
		return types.ArchiveLocatorDocx
	case ".xlsx", ".xls":
		return types.ArchiveLocatorSpreadsheet
	default:
		if archiveImageExtension(doc.FileType) {
			return types.ArchiveLocatorImage
		}
		return types.ArchiveLocatorText
	}
}

func appendArchiveFieldEvidence(doc *types.ArchiveDocument, content, field, value, quote string, confidence float64, evidence *[]*types.ArchiveFieldEvidence) {
	if doc == nil || strings.TrimSpace(value) == "" || evidence == nil {
		return
	}
	if strings.TrimSpace(quote) == "" {
		quote = value
	}
	start := strings.Index(content, quote)
	if start < 0 {
		start = strings.Index(strings.ToLower(content), strings.ToLower(quote))
	}
	if start < 0 {
		start = strings.Index(strings.ToLower(content), strings.ToLower(value))
	}
	if start < 0 {
		start = 0
	}
	end := start + len(quote)
	page := 1
	if archiveEvidenceLocatorKind(doc) != types.ArchiveLocatorImage && start > 0 {
		page += strings.Count(content[:start], "\f")
	}
	locator, _ := json.Marshal(map[string]any{"char_start": start, "char_end": end, "page": page, "quote": quote})
	*evidence = append(*evidence, &types.ArchiveFieldEvidence{TenantID: doc.TenantID, DocumentID: doc.ID, KnowledgeID: doc.KnowledgeID, FieldName: field, Value: strings.TrimSpace(value), Confidence: confidence, Quote: quote, LocatorKind: archiveEvidenceLocatorKind(doc), Locator: types.JSON(locator), SourceStart: start, SourceEnd: end})
}

// extractArchiveTableFields recovers the common Markdown table shape emitted
// by the VLM for scanned outbound/loan documents. It is intentionally
// conservative: only columns with an explicit header are accepted, so a
// random number in a clause cannot become a serial number or quantity.
func extractArchiveTableFields(content string) map[string]struct{ value, quote string } {
	result := map[string]struct{ value, quote string }{}
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		if !strings.Contains(line, "|") || index+1 >= len(lines) {
			continue
		}
		parts := splitArchiveTableRow(line)
		if len(parts) < 2 {
			continue
		}
		next := splitArchiveTableRow(lines[index+1])
		if len(next) != len(parts) || !isArchiveTableSeparator(lines[index+1]) {
			continue
		}
		indices := map[string]int{}
		for column, header := range parts {
			header = strings.ToLower(strings.TrimSpace(header))
			switch {
			case strings.Contains(header, "物料名称") || strings.Contains(header, "设备名称") || strings.Contains(header, "产品名称"):
				indices["asset_name"] = column
			case strings.Contains(header, "物料参数") || strings.Contains(header, "规格型号") || strings.Contains(header, "型号"):
				indices["asset_model"] = column
			case strings.Contains(header, "序列") || header == "sn" || strings.Contains(header, "编码"):
				indices["serial_number"] = column
			case strings.Contains(header, "数量"):
				indices["quantity"] = column
			}
		}
		if len(indices) == 0 || index+2 >= len(lines) {
			continue
		}
		row := splitArchiveTableRow(lines[index+2])
		if len(row) != len(parts) {
			continue
		}
		for field, column := range indices {
			if column >= len(row) || strings.TrimSpace(row[column]) == "" {
				continue
			}
			result[field] = struct{ value, quote string }{value: strings.TrimSpace(row[column]), quote: strings.TrimSpace(lines[index+2])}
		}
		break
	}
	return result
}

func splitArchiveTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

func isArchiveTableSeparator(line string) bool {
	for _, part := range splitArchiveTableRow(line) {
		part = strings.TrimSpace(part)
		if part == "" || strings.Trim(part, "-:") != "" {
			return false
		}
	}
	return true
}

func extractArchiveFields(doc *types.ArchiveDocument, content string) (map[string]string, []*types.ArchiveFieldEvidence) {
	fields := map[string]string{}
	evidence := []*types.ArchiveFieldEvidence{}
	classifyArchiveDocument(doc, content)
	for _, def := range archiveFieldPatterns {
		re := regexp.MustCompile(def.pattern)
		match := re.FindStringSubmatch(content)
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if def.name == "customer" || def.name == "borrower" || def.name == "lender" {
			value = cleanArchiveEntityName(value)
		}
		if value == "" {
			continue
		}
		fields[def.name] = value
		appendArchiveFieldEvidence(doc, content, def.name, value, match[0], def.confidence, &evidence)
	}
	// For a loan agreement the borrower, rather than the lender (甲方), is the
	// related customer. Keep the original party evidence and add an explicit
	// customer evidence row for the borrower so linking and citations agree.
	if doc.DocumentType == types.ArchiveDocumentLoanAgreement && strings.TrimSpace(fields["borrower"]) != "" {
		fields["customer"] = fields["borrower"]
		appendArchiveFieldEvidence(doc, content, "customer", fields["borrower"], fields["borrower"], 0.94, &evidence)
	}
	for field, table := range extractArchiveTableFields(content) {
		if strings.TrimSpace(fields[field]) == "" {
			fields[field] = table.value
			appendArchiveFieldEvidence(doc, content, field, table.value, table.quote, 0.82, &evidence)
		}
	}
	// Many Chinese loan forms put the serial number in parentheses after the
	// model (for example “SE50221（ES33000180）”) instead of giving it a
	// dedicated SN column. Treat that shape as a lower-confidence, traceable
	// serial candidate rather than losing the asset identity entirely.
	if strings.TrimSpace(fields["serial_number"]) == "" && strings.TrimSpace(fields["asset_model"]) != "" {
		if match := regexp.MustCompile(`[（(]\s*([A-Za-z0-9][A-Za-z0-9_\-]*)\s*[）)]`).FindStringSubmatch(fields["asset_model"]); len(match) == 2 {
			fields["serial_number"] = match[1]
			appendArchiveFieldEvidence(doc, content, "serial_number", match[1], fields["asset_model"], 0.74, &evidence)
		}
	}
	if amount := strings.ReplaceAll(strings.ReplaceAll(fields["amount"], ",", ""), " ", ""); amount != "" {
		currency := strings.TrimRight(amount, "0123456789.")
		if currency != "" {
			doc.Currency = strings.ToUpper(currency)
		}
		amount = strings.TrimLeft(amount, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
		if value, err := strconv.ParseFloat(amount, 64); err == nil {
			doc.Amount = value
		}
	}
	for _, key := range []string{"signed_at", "effective_at", "expires_at", "return_due_at", "returned_at", "renewed_at", "payment_at", "delivery_at"} {
		if date, err := parseArchiveFieldDate(key, fields[key]); err == nil {
			switch key {
			case "signed_at":
				doc.SignedAt = &date
			case "effective_at":
				doc.EffectiveAt = &date
			case "expires_at":
				doc.ExpiresAt = &date
			case "return_due_at":
				doc.ReturnDueAt = &date
			case "returned_at":
				doc.ReturnedAt = &date
			case "renewed_at":
				doc.RenewedAt = &date
			}
		}
	}
	doc.AgreementNumber = fields["agreement_number"]
	return fields, evidence
}
