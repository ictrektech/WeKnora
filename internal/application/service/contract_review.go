package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

var (
	ErrContractReviewNotFound     = errors.New("contract review not found")
	ErrContractReviewInvalidState = errors.New("contract review is not in a valid state for this action")
	ErrContractReviewInvalidFile  = errors.New("only PDF and DOCX contracts are supported")
	ErrContractReviewInvalidModel = errors.New("contract review model must be an active knowledge QA model")
	ErrContractReviewModelMissing = errors.New("no contract review model is configured")
)

var contractReviewPlaybooks = []types.ContractReviewPlaybook{{
	ID: "general-contract-review", Name: "General Contract Review",
	Description: "A balanced review of commercial, legal, operational, and drafting risk.", Version: "1.0",
}}

const (
	contractReviewClauseSystemPrompt = "You are a senior commercial contracts lawyer reviewing a primary contract-text window together with cross-window context from the same contract. The primary window may contain several legal sections, repeated descriptions, and page-break artifacts; its title is only an analysis-window label, not a legal clause number. Read the entire primary window, including headings and nearby provisions. Use cross-window context to check whether another explicit provision covers, limits, or contradicts a requirement. Distinguish a real omission from a contradiction or a genuinely ambiguous rule. Every issue must be supported by an exact passage in at least one supplied evidence unit; prefer the primary unit for issues about this window, but a supporting unit is allowed when it contains the exact passage establishing coverage, contradiction, or a related requirement. If two observations are the same underlying risk, return one consolidated issue. Return exactly one JSON object and nothing else. Do not output markdown, code fences, headings, citations, a full-contract report, or reasoning text. Return at most five issues. Contract facts are extracted by the service from the full source after clause review; do not extract facts in this response and always return facts as an empty array. " +
		"Every issue must include category, finding_type, risk_level, title, explanation, original_quote, suggestion, and evidence_refs. Use only evidence_id values supplied in the prompt. Write issue titles, explanations, and suggestions in Simplified Chinese. Keep original_quote exactly in the contract's original language and wording. Copy a contiguous exact passage from the cited evidence unit. Make it long and distinctive enough to identify one location; include a nearby heading, clause number, or other unique context when a short phrase is repeated. If no unique exact passage is available, omit the issue."
	contractReviewOverviewSystemPrompt = "Return a concise, evidence-based contract review overview as valid JSON only. Do not decide the final risk level; it is computed by the service. Write executive_summary, contract_type, and key_recommendations in Simplified Chinese. Preserve party names and other proper nouns in their original form. Summarize only the supplied verified issues and facts."
)

const (
	contractReviewClauseDefaultMaxCompletionTokens = 4096
	// Give the recovery attempt enough room to finish a valid JSON response
	// when the model uses the normal clause budget for an overly verbose first
	// response. The first attempt remains bounded at the agent-configured
	// budget; only a failed/truncated response gets the larger retry budget.
	contractReviewClauseRetryMaxCompletionTokens = 8192
	contractReviewClauseChunkSize                = 2800
	contractReviewClauseChunkOverlap             = 120
	contractReviewFullDocumentContextMaxRunes    = 12000
	contractReviewRelatedContextMaxRunes         = 3600
	contractReviewRelatedPassageMaxRunes         = 900
	contractReviewDocumentOutlineMaxRunes        = 1400
	contractReviewAnalysisTimeout                = 30 * time.Minute
	contractReviewAnalysisMaxRetries             = 1
	contractReviewStaleGrace                     = 10 * time.Minute
	contractReviewFailurePersistTimeout          = 5 * time.Second
	contractReviewCancelQueueTimeout             = 2 * time.Second
)

func contractReviewPlaybook(id string) (types.ContractReviewPlaybook, bool) {
	for _, playbook := range contractReviewPlaybooks {
		if playbook.ID == id {
			return playbook, true
		}
	}
	return types.ContractReviewPlaybook{}, false
}

type contractReviewService struct {
	repo      interfaces.ContractReviewRepository
	files     interfaces.FileService
	resources interfaces.ResourceCatalog
	reader    interfaces.DocumentReader
	models    interfaces.ModelService
	agents    interfaces.CustomAgentService
	tasks     interfaces.TaskEnqueuer
	inspector interfaces.ContractReviewTaskInspector
}

func NewContractReviewService(repo interfaces.ContractReviewRepository, files interfaces.FileService,
	resources interfaces.ResourceCatalog, reader interfaces.DocumentReader, models interfaces.ModelService,
	agents interfaces.CustomAgentService, tasks interfaces.TaskEnqueuer,
	inspector interfaces.ContractReviewTaskInspector) interfaces.ContractReviewService {
	return &contractReviewService{repo: repo, files: files, resources: resources, reader: reader, models: models, agents: agents, tasks: tasks, inspector: inspector}
}

func (s *contractReviewService) Playbooks() []types.ContractReviewPlaybook {
	return append([]types.ContractReviewPlaybook(nil), contractReviewPlaybooks...)
}

func (s *contractReviewService) Create(ctx context.Context, tenantID uint64, userID string) (*types.ContractReview, error) {
	r := &types.ContractReview{TenantID: tenantID, UserID: userID}
	if err := s.repo.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *contractReviewService) List(ctx context.Context, tenantID uint64, userID string, archived bool) ([]*types.ContractReview, error) {
	rows, err := s.repo.List(ctx, tenantID, userID, archived)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		normalizeContractReviewForRead(row)
	}
	return rows, nil
}

func (s *contractReviewService) Get(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	r, err := s.repo.Get(ctx, tenantID, userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrContractReviewNotFound
	}
	if err == nil {
		normalizeContractReviewForRead(r)
	}
	return r, err
}

func normalizeContractReviewForRead(review *types.ContractReview) {
	if review == nil {
		return
	}
	if review.QualityStatus == "" {
		if isSettledContractReviewStatus(review.Status) {
			review.QualityStatus = types.ContractReviewQualityLegacy
		} else {
			review.QualityStatus = types.ContractReviewQualityPending
		}
	}
	if len(review.Locator) == 0 {
		review.Locator = types.JSON(`{}`)
	}
	if len(review.Warnings) == 0 {
		review.Warnings = types.JSON(`[]`)
	}
	// Get() preloads these collections. A settled empty slice therefore means
	// "loaded and no rows", while a running nil slice still means that the
	// stream has not delivered the findings yet.
	if review.Issues == nil && isSettledContractReviewStatus(review.Status) {
		review.Issues = []*types.ContractReviewIssue{}
	}
	if review.Clauses == nil && isSettledContractReviewStatus(review.Status) {
		review.Clauses = []*types.ContractReviewClause{}
	}
	if review.SourceTextHash == "" {
		review.SourceTextHash = review.SourceHash
	}
	for _, clause := range review.Clauses {
		if clause != nil && clause.SourceRevision == "" {
			clause.SourceRevision = review.SourceRevision
		}
	}
	locatorUnits := contractReviewLocatorUnits(review.Locator)
	for _, issue := range review.Issues {
		if issue == nil {
			continue
		}
		issue.SourceRevision = review.SourceRevision
		var refs []string
		if len(issue.EvidenceRefs) > 0 {
			_ = json.Unmarshal(issue.EvidenceRefs, &refs)
		}
		issue.Evidence = make([]types.ContractReviewEvidence, 0, len(refs))
		for index, ref := range uniqueReviewStrings(refs) {
			role := types.ContractReviewEvidenceSupporting
			if index == 0 {
				role = types.ContractReviewEvidencePrimary
			}
			status := "unsupported"
			evidenceStart, evidenceEnd := 0, 0
			pageStart, pageEnd := 0, 0
			if unit, ok := locatorUnits[ref]; ok {
				status = "located"
				evidenceStart, evidenceEnd = unit.SourceStart, unit.SourceEnd
				pageStart, pageEnd = unit.Page, unit.Page
			} else if index == 0 && contractReviewLocatorSupportsRange(locatorUnits, issue.SourceStart, issue.SourceEnd, review.ExtractedContent) {
				// Analysis evidence IDs are derived from source ranges and are
				// intentionally distinct from parser unit IDs. The primary issue
				// range remains authoritative when a parser unit contains it.
				status = "located"
				evidenceStart, evidenceEnd = issue.SourceStart, issue.SourceEnd
			}
			if review.QualityStatus == types.ContractReviewQualityLegacy || review.SourceRevision == "" {
				status = "unsupported"
			}
			issue.Evidence = append(issue.Evidence, types.ContractReviewEvidence{
				EvidenceID: ref, Role: role, SourceRevision: review.SourceRevision,
				SourceTextHash: review.SourceTextHash, SourceStart: evidenceStart,
				SourceEnd: evidenceEnd, PageStart: pageStart, PageEnd: pageEnd, Status: status,
			})
		}
		switch {
		case len(issue.Evidence) == 0 && review.QualityStatus == types.ContractReviewQualityLegacy:
			issue.EvidenceStatus = "unsupported"
		case len(issue.Evidence) == 0:
			issue.EvidenceStatus = "not_found"
		default:
			issue.EvidenceStatus = issue.Evidence[0].Status
		}
	}
}

func validParty(value string) bool {
	switch types.ContractReviewParty(value) {
	case types.ContractReviewPartyCustomer, types.ContractReviewPartyVendor, types.ContractReviewPartyNeutral:
		return true
	default:
		return false
	}
}

func canRetryContractReview(status types.ContractReviewStatus) bool {
	return status == types.ContractReviewStatusFailed || status == types.ContractReviewStatusCompleted || status == types.ContractReviewStatusCancelled
}

func isRunningContractReviewStatus(status types.ContractReviewStatus) bool {
	return status == types.ContractReviewStatusUploading || status == types.ContractReviewStatusAnalyzing || status == types.ContractReviewStatusReviewingClauses
}

func isSettledContractReviewStatus(status types.ContractReviewStatus) bool {
	return status == types.ContractReviewStatusCompleted || status == types.ContractReviewStatusFailed || status == types.ContractReviewStatusCancelled
}

func contractReviewStaleThreshold() time.Duration {
	// One full attempt, the configured retry budget, and a small scheduling
	// buffer. The worker timeout normally persists failure immediately; this is
	// the last-resort bound for a worker/process that disappears before it can.
	return contractReviewAnalysisTimeout*time.Duration(contractReviewAnalysisMaxRetries+1) + contractReviewStaleGrace
}

func canUpdateContractReviewConfig(status types.ContractReviewStatus) bool {
	return status == types.ContractReviewStatusDraft || status == types.ContractReviewStatusReady || status == types.ContractReviewStatusCompleted
}

func contractReviewTaskMatchesRun(p types.ContractReviewTaskPayload, review *types.ContractReview) bool {
	if review == nil {
		return false
	}
	if strings.TrimSpace(p.AnalysisRunID) == "" || p.AnalysisRunID != review.AnalysisRunID {
		return false
	}
	if strings.TrimSpace(p.ConfigHash) != "" && p.ConfigHash != review.ConfigHash {
		return false
	}
	return true
}

func (s *contractReviewService) updateReviewForRun(ctx context.Context, review *types.ContractReview, runID string) error {
	if strings.TrimSpace(runID) == "" {
		return s.repo.Update(ctx, review)
	}
	if err := s.repo.UpdateForRun(ctx, review, runID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errContractReviewStaleRun
		}
		return err
	}
	return nil
}

func (s *contractReviewService) Update(ctx context.Context, tenantID uint64, userID, id, title, playbook, party string, modelID *string, archived *bool) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	previousPlaybook, previousParty, previousModel := r.PlaybookID, r.RepresentedParty, r.ModelID
	if strings.TrimSpace(title) != "" {
		r.Title = strings.TrimSpace(title)
		r.TitleCustomized = true
	}
	if playbook != "" {
		selected, ok := contractReviewPlaybook(playbook)
		if !ok {
			return nil, fmt.Errorf("unknown playbook")
		}
		if !canUpdateContractReviewConfig(r.Status) {
			return nil, ErrContractReviewInvalidState
		}
		r.PlaybookID, r.PlaybookVersion = selected.ID, selected.Version
	}
	if party != "" {
		if !validParty(party) {
			return nil, fmt.Errorf("invalid represented party")
		}
		if !canUpdateContractReviewConfig(r.Status) {
			return nil, ErrContractReviewInvalidState
		}
		r.RepresentedParty = types.ContractReviewParty(party)
	}
	if modelID != nil {
		if !canUpdateContractReviewConfig(r.Status) {
			return nil, ErrContractReviewInvalidState
		}
		selectedModelID := strings.TrimSpace(*modelID)
		if selectedModelID != "" {
			if err := s.validateReviewModel(ctx, tenantID, userID, selectedModelID); err != nil {
				return nil, err
			}
		}
		r.ModelID = selectedModelID
	}
	if r.Status == types.ContractReviewStatusCompleted &&
		(previousPlaybook != r.PlaybookID || previousParty != r.RepresentedParty || previousModel != r.ModelID) {
		r.AnalysisRunID, r.ConfigHash = "", ""
		r.QualityStatus = types.ContractReviewQualityStale
		warning := types.ContractReviewWarning{Code: "CONFIG_CHANGED", Message: "审查配置已变更，现有结果需要重新审查"}
		warnings, warningErr := appendContractReviewWarning(r.Warnings, warning)
		if warningErr != nil {
			return nil, warningErr
		}
		r.Warnings = warnings
		if updatedOverview, overviewErr := updateContractReviewOverviewQuality(r.Overview, r.QualityStatus, warnings); overviewErr == nil {
			r.Overview = updatedOverview
		}
	}
	if archived != nil {
		if r.Status == types.ContractReviewStatusUploading || r.Status == types.ContractReviewStatusAnalyzing || r.Status == types.ContractReviewStatusReviewingClauses {
			return nil, ErrContractReviewInvalidState
		}
		if *archived {
			now := time.Now()
			r.ArchivedAt = &now
		} else {
			r.ArchivedAt = nil
		}
	}
	if err := s.repo.Update(ctx, r); err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, userID, id)
}

// Cancel marks a live review run terminal before touching the queue. The
// conditional run update makes the database row win any race with a worker:
// once it is cancelled, stale workers can no longer persist progress, clauses,
// issues, or a late completed/failed result for that run.
func (s *contractReviewService) Cancel(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if !isRunningContractReviewStatus(r.Status) {
		return nil, ErrContractReviewInvalidState
	}
	r.Status = types.ContractReviewStatusCancelled
	r.ErrorMessage = "合同审查已取消，可重试。"
	r.QualityStatus = types.ContractReviewQualityInvalid
	now := time.Now()
	r.CompletedAt = &now
	if err := s.updateReviewForRun(ctx, r, r.AnalysisRunID); err != nil {
		if errors.Is(err, errContractReviewStaleRun) {
			// A concurrent cancel/retry already settled this run. Return the
			// durable row so the endpoint remains idempotent for the user.
			return s.Get(ctx, tenantID, userID, id)
		}
		return nil, err
	}
	s.cancelReviewTasks(ctx, r)
	return s.Get(ctx, tenantID, userID, id)
}

func (s *contractReviewService) cancelReviewTasks(ctx context.Context, review *types.ContractReview) {
	if s.inspector == nil || review == nil {
		return
	}
	queueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), contractReviewCancelQueueTimeout)
	defer cancel()
	if _, _, err := s.inspector.CancelTasksForContractReview(queueCtx, review.ID, review.AnalysisRunID); err != nil {
		logger.Warnf(queueCtx, "[ContractReview] cancel queued tasks review=%s run=%s failed: %v", review.ID, review.AnalysisRunID, err)
	}
}

func (s *contractReviewService) Delete(ctx context.Context, tenantID uint64, userID, id string) error {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return err
	}
	if isRunningContractReviewStatus(r.Status) {
		// Deletion is allowed for compatibility with the existing UI. Stop
		// queued work first; the row deletion itself prevents late writes.
		s.cancelReviewTasks(ctx, r)
	}
	if r.ResourceRef != "" {
		_ = s.files.DeleteFile(ctx, r.ResourceRef)
	}
	return s.repo.Delete(ctx, tenantID, userID, id)
}

// DeleteTenantData removes all contract-review database data for a tenant and
// then cleans source objects. The database operation is atomic and commits
// before external storage is touched. If storage cleanup fails, the method
// returns the combined error. Repeating the operation is idempotent for the
// database state; a provider failure after the commit is surfaced to the
// caller so the storage cleanup can be handled separately.
func (s *contractReviewService) DeleteTenantData(ctx context.Context, tenantID uint64) error {
	resources, err := s.repo.DeleteTenantData(ctx, tenantID)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(resources))
	var cleanupErr error
	for _, resource := range resources {
		reference := strings.TrimSpace(resource.Reference)
		if reference == "" {
			continue
		}
		if _, ok := seen[reference]; ok {
			continue
		}
		seen[reference] = struct{}{}

		remaining := int64(-1)
		if s.resources != nil {
			remaining, err = s.resources.Release(ctx, reference, types.ResourceOwnerContractReview, resource.ReviewID)
			if err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("release contract review resource %q: %w", reference, err))
				continue
			}
		}
		// A shared resource remains owned by another domain object. Raw legacy
		// paths return -1 from Release and retain the historical delete path.
		if remaining > 0 {
			continue
		}
		if s.files == nil {
			cleanupErr = errors.Join(cleanupErr, errors.New("contract review file service is not configured"))
			continue
		}
		if err := s.files.DeleteFile(ctx, reference); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete contract review resource %q: %w", reference, err))
		}
	}
	return cleanupErr
}

func (s *contractReviewService) BulkAction(ctx context.Context, tenantID uint64, userID string, ids []string, action types.ContractReviewBulkAction) (*types.ContractReviewBulkResult, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, fmt.Errorf("contract review bulk action requires between 1 and 500 ids")
	}
	if action != types.ContractReviewBulkArchive && action != types.ContractReviewBulkRestore && action != types.ContractReviewBulkDelete {
		return nil, fmt.Errorf("invalid contract review bulk action")
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("contract review bulk action requires at least one valid id")
	}
	result := &types.ContractReviewBulkResult{Action: action, Requested: len(unique), Items: make([]types.ContractReviewBulkItem, 0, len(unique))}
	for _, id := range unique {
		var err error
		if action == types.ContractReviewBulkDelete {
			err = s.Delete(ctx, tenantID, userID, id)
		} else {
			archived := action == types.ContractReviewBulkArchive
			_, err = s.Update(ctx, tenantID, userID, id, "", "", "", nil, &archived)
		}
		item := types.ContractReviewBulkItem{ID: id, Success: err == nil}
		if err != nil {
			item.Error = err.Error()
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *contractReviewService) Upload(ctx context.Context, tenantID uint64, userID, id, fileName, mimeType string, fileSize int64, body io.Reader) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if r.Status != types.ContractReviewStatusDraft && r.Status != types.ContractReviewStatusReady && r.Status != types.ContractReviewStatusFailed {
		return nil, ErrContractReviewInvalidState
	}
	safe, err := secutils.SafeFileName(fileName)
	if err != nil {
		return nil, ErrContractReviewInvalidFile
	}
	ext := strings.ToLower(filepath.Ext(safe))
	if ext != ".pdf" && ext != ".docx" {
		return nil, ErrContractReviewInvalidFile
	}
	max := secutils.GetMaxFileSizeMB() * 1024 * 1024
	if fileSize <= 0 || fileSize > max {
		return nil, fmt.Errorf("file size must be between 1 byte and %dMB", secutils.GetMaxFileSizeMB())
	}
	data, err := io.ReadAll(io.LimitReader(body, max+1))
	if err != nil {
		return nil, fmt.Errorf("read contract file: %w", err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file exceeds size limit of %dMB", secutils.GetMaxFileSizeMB())
	}
	if (ext == ".pdf" && !strings.HasPrefix(string(data[:min(len(data), 5)]), "%PDF-")) ||
		(ext == ".docx" && (len(data) < 4 || string(data[:4]) != "PK\x03\x04")) {
		return nil, ErrContractReviewInvalidFile
	}
	ref, err := s.files.SaveBytes(ctx, data, tenantID, "contract_review_"+uuid.NewString()[:12]+ext, false)
	if err != nil {
		return nil, err
	}
	if s.resources != nil {
		if err := s.resources.Bind(ctx, ref, types.ResourceOwnerContractReview, r.ID, "source_file"); err != nil {
			_ = s.files.DeleteFile(ctx, ref)
			return nil, err
		}
	}
	oldRef := r.ResourceRef
	oldRunID := r.AnalysisRunID
	r.ResourceRef, r.FileName, r.FileType, r.MimeType, r.FileSize = ref, safe, ext, strings.TrimSpace(mimeType), int64(len(data))
	r.Status, r.Progress, r.ErrorMessage, r.ExtractedContent = types.ContractReviewStatusUploading, 5, "", ""
	r.AnalysisRunID, r.ConfigHash, r.SourceHash = newContractReviewAnalysisRunID(), "", ""
	r.SourceRevision, r.SourceTextHash = "", ""
	r.QualityStatus, r.Warnings, r.Locator = types.ContractReviewQualityPending, types.JSON(`[]`), types.JSON(`{}`)
	r.Overview = types.JSON(`{}`)
	if !r.TitleCustomized {
		r.Title = strings.TrimSuffix(safe, ext)
	}
	if err := s.clearReviewResults(ctx, r.ID, oldRunID); err != nil {
		_ = s.files.DeleteFile(ctx, ref)
		return nil, err
	}
	if err := s.repo.Update(ctx, r); err != nil {
		_ = s.files.DeleteFile(ctx, ref)
		return nil, err
	}
	if oldRef != "" {
		_ = s.files.DeleteFile(ctx, oldRef)
	}
	if err := s.enqueue(ctx, types.TypeContractReviewDocumentProcess, r, 2, 10*time.Minute); err != nil {
		r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, "failed to schedule document parsing"
		if updateErr := s.repo.Update(ctx, r); updateErr != nil {
			return r, fmt.Errorf("%w (also failed to persist scheduling failure: %v)", err, updateErr)
		}
		return r, err
	}
	return r, nil
}

func (s *contractReviewService) OpenDocument(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, io.ReadCloser, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, nil, err
	}
	if r.ResourceRef == "" {
		return nil, nil, ErrContractReviewNotFound
	}
	f, err := s.files.GetFile(ctx, r.ResourceRef)
	return r, f, err
}

func (s *contractReviewService) Start(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if r.Status != types.ContractReviewStatusReady {
		return nil, ErrContractReviewInvalidState
	}
	if r.SourceTextHash == "" && r.SourceHash == "" && strings.TrimSpace(r.ExtractedContent) != "" {
		r.SourceHash = contractReviewSourceHash(r.ExtractedContent)
	}
	if r.SourceTextHash == "" {
		r.SourceTextHash = r.SourceHash
	}
	if r.SourceRevision == "" && r.SourceTextHash != "" {
		r.SourceRevision = "text-v2:" + r.SourceTextHash
	}
	r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusAnalyzing, 20, ""
	r.AnalysisRunID = newContractReviewAnalysisRunID()
	r.ConfigHash = contractReviewConfigHash(r)
	r.QualityStatus = types.ContractReviewQualityPending
	r.Warnings = types.JSON(`[]`)
	now := time.Now()
	r.StartedAt, r.CompletedAt = &now, nil
	if err := s.repo.Update(ctx, r); err != nil {
		return nil, err
	}
	if err := s.enqueue(ctx, types.TypeContractReviewAnalyze, r, contractReviewAnalysisMaxRetries, contractReviewAnalysisTimeout); err != nil {
		r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, "failed to schedule contract review"
		if updateErr := s.repo.Update(ctx, r); updateErr != nil {
			return r, fmt.Errorf("%w (also failed to persist scheduling failure: %v)", err, updateErr)
		}
		return r, err
	}
	return r, nil
}

func (s *contractReviewService) Retry(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	r, err := s.Get(ctx, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if !canRetryContractReview(r.Status) {
		return nil, ErrContractReviewInvalidState
	}
	oldRunID := r.AnalysisRunID
	if err := s.clearReviewResults(ctx, r.ID, oldRunID); err != nil {
		return nil, err
	}
	r.AnalysisRunID = newContractReviewAnalysisRunID()
	if r.SourceTextHash == "" {
		r.SourceTextHash = r.SourceHash
	}
	if r.SourceRevision == "" && r.SourceTextHash != "" {
		r.SourceRevision = "text-v2:" + r.SourceTextHash
	}
	r.ConfigHash = contractReviewConfigHash(r)
	r.QualityStatus = types.ContractReviewQualityPending
	r.Warnings = types.JSON(`[]`)
	if r.ExtractedContent == "" {
		r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusUploading, 5, ""
		if err := s.repo.Update(ctx, r); err != nil {
			return nil, err
		}
		if err := s.enqueue(ctx, types.TypeContractReviewDocumentProcess, r, 2, 10*time.Minute); err != nil {
			r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, "failed to schedule document parsing"
			if updateErr := s.repo.Update(ctx, r); updateErr != nil {
				return r, fmt.Errorf("%w (also failed to persist scheduling failure: %v)", err, updateErr)
			}
			return r, err
		}
		return r, nil
	}
	r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusAnalyzing, 20, ""
	r.CompletedAt = nil
	r.Overview = types.JSON(`{}`)
	r.Clauses = nil
	r.Issues = nil
	now := time.Now()
	r.StartedAt = &now
	if err := s.repo.Update(ctx, r); err != nil {
		return nil, err
	}
	if err := s.enqueue(ctx, types.TypeContractReviewAnalyze, r, contractReviewAnalysisMaxRetries, contractReviewAnalysisTimeout); err != nil {
		r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, "failed to schedule contract review"
		if updateErr := s.repo.Update(ctx, r); updateErr != nil {
			return r, fmt.Errorf("%w (also failed to persist scheduling failure: %v)", err, updateErr)
		}
		return r, err
	}
	return r, nil
}

func (s *contractReviewService) enqueue(ctx context.Context, taskType string, r *types.ContractReview, retries int, timeout time.Duration) error {
	payload, err := json.Marshal(types.ContractReviewTaskPayload{TenantID: r.TenantID, UserID: r.UserID, ReviewID: r.ID, AnalysisRunID: r.AnalysisRunID, ConfigHash: r.ConfigHash})
	if err != nil {
		return fmt.Errorf("marshal contract review task payload: %w", err)
	}
	queue, ok := types.QueueForTaskType(taskType)
	if !ok {
		return fmt.Errorf("no queue configured for contract review task %q", taskType)
	}
	if s.tasks == nil {
		return errors.New("contract review task queue is not configured")
	}
	_, err = s.tasks.Enqueue(asynq.NewTask(taskType, payload), asynq.Queue(queue), asynq.MaxRetry(retries), asynq.Timeout(timeout))
	return err
}

func reviewTaskContext(ctx context.Context, p types.ContractReviewTaskPayload) context.Context {
	ctx = context.WithValue(ctx, types.TenantIDContextKey, p.TenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, p.UserID)
	return ctx
}

func (s *contractReviewService) ProcessDocument(ctx context.Context, task *asynq.Task) error {
	var p types.ContractReviewTaskPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return err
	}
	ctx = reviewTaskContext(ctx, p)
	r, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
	if err != nil {
		if errors.Is(err, ErrContractReviewNotFound) {
			return nil
		}
		return err
	}
	if !contractReviewTaskMatchesRun(p, r) {
		return nil
	}
	if r.Status != types.ContractReviewStatusUploading && !(r.Status == types.ContractReviewStatusFailed && r.ExtractedContent == "") {
		return nil
	}
	if r.Status == types.ContractReviewStatusFailed {
		r.Status, r.ErrorMessage = types.ContractReviewStatusUploading, ""
		if err := s.updateReviewForRun(ctx, r, r.AnalysisRunID); err != nil {
			if errors.Is(err, errContractReviewStaleRun) {
				return nil
			}
			return err
		}
	}
	f, err := s.files.GetFile(ctx, r.ResourceRef)
	if err != nil {
		return s.fail(ctx, r, err)
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return s.fail(ctx, r, err)
	}
	if s.reader == nil {
		return s.fail(ctx, r, errors.New("document reader is not configured"))
	}
	result, err := s.reader.Read(ctx, &types.ReadRequest{FileContent: data, FileName: r.FileName, FileType: strings.TrimPrefix(r.FileType, ".")})
	if err != nil {
		return s.fail(ctx, r, err)
	}
	if result == nil || strings.TrimSpace(result.Error) != "" {
		if result == nil {
			return s.fail(ctx, r, errors.New("document reader returned no result"))
		}
		return s.fail(ctx, r, errors.New(result.Error))
	}
	latest, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
	if err != nil {
		// A user may delete a review while parsing is in flight. Never recreate
		// or update the soft-deleted aggregate after the parser returns.
		if errors.Is(err, ErrContractReviewNotFound) {
			return nil
		}
		return err
	}
	if !contractReviewTaskMatchesRun(p, latest) {
		return nil
	}
	r = latest
	meta, err := json.Marshal(result.Metadata)
	if err != nil {
		return s.fail(ctx, r, fmt.Errorf("marshal document metadata: %w", err))
	}
	// Keep the parser's exact assembled text. Source-unit offsets and the text
	// hash are defined over this value; trimming here would shift every locator
	// after a leading/trailing paragraph separator.
	r.ExtractedContent, r.Metadata = result.MarkdownContent, types.JSON(meta)
	if strings.TrimSpace(r.ExtractedContent) == "" {
		return s.fail(ctx, r, errors.New("the contract contains no extractable text"))
	}
	r.SourceHash = contractReviewSourceHash(r.ExtractedContent)
	r.SourceTextHash = r.SourceHash
	r.SourceRevision = firstNonEmptyReviewString(result.Metadata["source_revision"], "text-v2:"+r.SourceTextHash)
	locator, err := buildContractReviewLocator(r.SourceTextHash, r.ExtractedContent, result.Metadata)
	if err != nil {
		return s.fail(ctx, r, fmt.Errorf("build contract locator: %w", err))
	}
	var locatorMap map[string]any
	if err := json.Unmarshal(locator, &locatorMap); err != nil {
		return s.fail(ctx, r, fmt.Errorf("decode contract locator: %w", err))
	}
	locatorMap["source_revision"] = r.SourceRevision
	locatorMap["source_text_hash"] = r.SourceTextHash
	locatorBytes, err := json.Marshal(locatorMap)
	if err != nil {
		return s.fail(ctx, r, fmt.Errorf("marshal contract locator: %w", err))
	}
	locator = types.JSON(locatorBytes)
	r.Locator = locator
	r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusReady, 15, ""
	r.QualityStatus = types.ContractReviewQualityPending
	r.ConfigHash = contractReviewConfigHash(r)
	return s.updateReviewForRun(ctx, r, r.AnalysisRunID)
}

func (s *contractReviewService) fail(ctx context.Context, r *types.ContractReview, cause error) error {
	r.Status, r.ErrorMessage = types.ContractReviewStatusFailed, contractReviewFailureMessage(cause)
	r.QualityStatus = types.ContractReviewQualityInvalid
	// Asynq cancels the handler context when a task reaches its timeout. The
	// failure state must still be durable, so use a short detached context for
	// this final write. UpdateForRun's active-status predicate prevents this
	// write from overwriting a user cancellation that won the race.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), contractReviewFailurePersistTimeout)
	defer cancel()
	if err := s.updateReviewForRun(persistCtx, r, r.AnalysisRunID); err != nil {
		if errors.Is(err, errContractReviewStaleRun) {
			return nil
		}
		return fmt.Errorf("%w (also failed to persist failure state: %v)", cause, err)
	}
	return cause
}

func contractReviewFailureMessage(cause error) string {
	if cause == nil {
		return "合同审查失败，可重试。"
	}
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		return "合同审查任务超时，可重试。"
	case errors.Is(cause, context.Canceled):
		return "合同审查任务已取消，可重试。"
	default:
		return cause.Error()
	}
}

func (s *contractReviewService) clearReviewResults(ctx context.Context, reviewID, runID string) error {
	if strings.TrimSpace(runID) == "" {
		return s.repo.ClearResults(ctx, reviewID)
	}
	return s.repo.ClearResultsForRun(ctx, reviewID, runID)
}

func contractReviewModelContext(ctx context.Context, tenantID uint64, userID string) context.Context {
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	if userID != "" {
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	return ctx
}

func (s *contractReviewService) validateReviewModel(ctx context.Context, tenantID uint64, userID, modelID string) error {
	if s.models == nil || strings.TrimSpace(modelID) == "" {
		return ErrContractReviewInvalidModel
	}
	model, err := s.models.GetModelByID(contractReviewModelContext(ctx, tenantID, userID), strings.TrimSpace(modelID))
	if err != nil || model == nil || model.Type != types.ModelTypeKnowledgeQA || model.Status != types.ModelStatusActive {
		return ErrContractReviewInvalidModel
	}
	return nil
}

type reviewIssueOutput struct {
	Category      string   `json:"category"`
	FindingType   string   `json:"finding_type"`
	RiskLevel     string   `json:"risk_level"`
	Title         string   `json:"title"`
	Explanation   string   `json:"explanation"`
	OriginalQuote string   `json:"original_quote"`
	Suggestion    string   `json:"suggestion"`
	EvidenceRefs  []string `json:"evidence_refs"`

	resolvedStart  int    `json:"-"`
	resolvedEnd    int    `json:"-"`
	canonicalQuote string `json:"-"`
}
type reviewBatchOutput struct {
	Issues []reviewIssueOutput `json:"issues"`
	Facts  []reviewFactOutput  `json:"facts"`
}
type reviewOverviewOutput struct {
	OverallRisk        string   `json:"overall_risk"`
	ExecutiveSummary   string   `json:"executive_summary"`
	ContractType       string   `json:"contract_type"`
	Parties            []string `json:"parties"`
	KeyRecommendations []string `json:"key_recommendations"`
}

var (
	markdownHeading       = regexp.MustCompile(`(?m)^#{1,4}\s+(.+)$`)
	reviewNumberedHeading = regexp.MustCompile(`^(?:[一二三四五六七八九十百千万]+、\s*|\d+(?:\.\d+)+\s*)\S`)
)

type reviewDocumentSection struct {
	start      int
	end        int
	heading    string
	text       string
	normalized string
}

type reviewDocumentContext struct {
	runes        []rune
	sections     []reviewDocumentSection
	outline      []string
	termSections map[string][]int
}

func reviewHeadingText(line string) string {
	trimmed := strings.TrimSpace(line)
	if match := markdownHeading.FindStringSubmatch(trimmed); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	if reviewNumberedHeading.MatchString(trimmed) {
		return trimmed
	}
	return ""
}

// reviewContextSearchTerms extracts lightweight lexical anchors without
// relying on a language-specific tokenizer. Chinese trigrams and ASCII runs
// provide generic lexical overlap for locating related sections.
func reviewContextSearchTerms(value string) map[string]int {
	normalized := normalizeReviewQuoteText(value)
	terms := make(map[string]int)

	runes := []rune(normalized)
	for index := 0; index+2 < len(runes); index++ {
		if unicode.Is(unicode.Han, runes[index]) && unicode.Is(unicode.Han, runes[index+1]) && unicode.Is(unicode.Han, runes[index+2]) {
			term := string(runes[index : index+3])
			if terms[term] < 1 {
				terms[term] = 1
			}
		}
	}
	for index := 0; index < len(runes); {
		if !isReviewWordRune(runes[index]) {
			index++
			continue
		}
		start := index
		hasLetter := false
		for index < len(runes) && isReviewWordRune(runes[index]) {
			hasLetter = hasLetter || unicode.IsLetter(runes[index])
			index++
		}
		if index-start >= 2 && hasLetter {
			term := string(runes[start:index])
			if terms[term] < 1 {
				terms[term] = 1
			}
		}
	}
	return terms
}

func isReviewWordRune(value rune) bool {
	return unicode.IsDigit(value) || (unicode.IsLetter(value) && !unicode.Is(unicode.Han, value))
}

func appendReviewDocumentSection(ctx *reviewDocumentContext, start, end int, heading string) {
	if start < 0 {
		start = 0
	}
	if end > len(ctx.runes) {
		end = len(ctx.runes)
	}
	if start >= end {
		return
	}
	text := string(ctx.runes[start:end])
	if strings.TrimSpace(text) == "" {
		return
	}
	ctx.sections = append(ctx.sections, reviewDocumentSection{
		start:      start,
		end:        end,
		heading:    heading,
		text:       text,
		normalized: normalizeReviewQuoteText(text),
	})
}

func newReviewDocumentContext(document string) *reviewDocumentContext {
	ctx := &reviewDocumentContext{runes: []rune(document), termSections: make(map[string][]int)}
	sectionStart := 0
	sectionHeading := ""
	for lineStart := 0; lineStart < len(ctx.runes); {
		lineEnd := lineStart
		for lineEnd < len(ctx.runes) && ctx.runes[lineEnd] != '\n' {
			lineEnd++
		}
		line := string(ctx.runes[lineStart:lineEnd])
		if heading := reviewHeadingText(line); heading != "" {
			appendReviewDocumentSection(ctx, sectionStart, lineStart, sectionHeading)
			sectionStart = lineStart
			sectionHeading = heading
			if len(ctx.outline) == 0 || ctx.outline[len(ctx.outline)-1] != heading {
				ctx.outline = append(ctx.outline, heading)
			}
		}
		if lineEnd == len(ctx.runes) {
			break
		}
		lineStart = lineEnd + 1
	}
	appendReviewDocumentSection(ctx, sectionStart, len(ctx.runes), sectionHeading)
	if len(ctx.sections) == 0 && len(ctx.runes) > 0 {
		appendReviewDocumentSection(ctx, 0, len(ctx.runes), "")
	}
	for index := range ctx.sections {
		for term := range reviewContextSearchTerms(ctx.sections[index].text) {
			ctx.termSections[term] = append(ctx.termSections[term], index)
		}
	}
	return ctx
}

func truncateReviewRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes == 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func addReviewContextPart(parts *[]string, used *int, value string, maxRunes int) bool {
	value = strings.TrimSpace(value)
	if value == "" || *used >= maxRunes {
		return false
	}
	prefix := ""
	if len(*parts) > 0 {
		prefix = "\n\n"
	}
	available := maxRunes - *used - len([]rune(prefix))
	if available <= 0 {
		return false
	}
	value = truncateReviewRunes(value, available)
	if value == "" {
		return false
	}
	*parts = append(*parts, prefix+value)
	*used += len([]rune(prefix)) + len([]rune(value))
	return true
}

func reviewContextSectionSnippet(section reviewDocumentSection, terms map[string]int, maxRunes int) string {
	text := strings.TrimSpace(section.text)
	if text == "" {
		return ""
	}
	textRunes := []rune(text)
	if len(textRunes) <= maxRunes {
		return text
	}

	matchStart := -1
	matchWeight := -1
	for term, weight := range terms {
		ranges := findReviewQuoteRanges(text, term)
		if len(ranges) == 0 {
			continue
		}
		start := ranges[0].Start
		if weight > matchWeight || (weight == matchWeight && (matchStart < 0 || start < matchStart)) {
			matchStart, matchWeight = start, weight
		}
	}
	if matchStart < 0 {
		return truncateReviewRunes(text, maxRunes)
	}

	start := matchStart - maxRunes/2
	if start < 0 {
		start = 0
	}
	if start+maxRunes > len(textRunes) {
		start = len(textRunes) - maxRunes
	}
	if start < 0 {
		start = 0
	}
	return truncateReviewRunes(string(textRunes[start:start+maxRunes]), maxRunes)
}

func reviewContextOutsideWindow(runes []rune, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return strings.TrimSpace(string(runes))
	}
	parts := make([]string, 0, 2)
	if start > 0 {
		parts = append(parts, strings.TrimSpace(string(runes[:start])))
	}
	if end < len(runes) {
		parts = append(parts, strings.TrimSpace(string(runes[end:])))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// CrossWindowContext returns enough contract-wide context for the current
// analysis window to reconcile provisions that were split apart. Short
// contracts receive all text outside the primary window. Long contracts use
// the document outline and ranked, semantically related sections so prompt
// size stays bounded.
func (ctx *reviewDocumentContext) CrossWindowContext(clause *types.ContractReviewClause) string {
	if ctx == nil || clause == nil || len(ctx.runes) == 0 {
		return ""
	}
	start, end := clause.SourceStart, clause.SourceEnd
	if start < 0 {
		start = 0
	}
	if end > len(ctx.runes) {
		end = len(ctx.runes)
	}
	if start >= end {
		return ""
	}
	if len(ctx.runes) <= contractReviewFullDocumentContextMaxRunes {
		outside := reviewContextOutsideWindow(ctx.runes, start, end)
		if outside == "" {
			return ""
		}
		return "Other contract text outside this analysis window:\n" + outside
	}

	body := string(ctx.runes[start:end])
	terms := reviewContextSearchTerms(body)
	scores := make([]int, len(ctx.sections))
	for term, weight := range terms {
		for _, sectionIndex := range ctx.termSections[term] {
			section := ctx.sections[sectionIndex]
			if section.start >= start && section.end <= end {
				continue
			}
			scores[sectionIndex] += weight
		}
	}

	indices := make([]int, 0, len(ctx.sections))
	for index, score := range scores {
		if score > 0 {
			indices = append(indices, index)
		}
	}
	sort.SliceStable(indices, func(left, right int) bool {
		if scores[indices[left]] != scores[indices[right]] {
			return scores[indices[left]] > scores[indices[right]]
		}
		return ctx.sections[indices[left]].start < ctx.sections[indices[right]].start
	})

	parts := make([]string, 0, 1+len(indices))
	used := 0
	if len(ctx.outline) > 0 {
		outline := "Document structure:\n" + strings.Join(ctx.outline, "\n")
		addReviewContextPart(&parts, &used, outline, contractReviewRelatedContextMaxRunes)
	}
	for _, index := range indices {
		section := ctx.sections[index]
		snippet := reviewContextSectionSnippet(section, terms, contractReviewRelatedPassageMaxRunes)
		if section.heading != "" {
			snippet = "Related provision [" + section.heading + "]:\n" + snippet
		}
		if !addReviewContextPart(&parts, &used, snippet, contractReviewRelatedContextMaxRunes) {
			break
		}
	}
	if len(parts) == 0 {
		neighborStart, neighborEnd := start-600, end+600
		if neighborStart < 0 {
			neighborStart = 0
		}
		if neighborEnd > len(ctx.runes) {
			neighborEnd = len(ctx.runes)
		}
		neighbor := reviewContextOutsideWindow(ctx.runes[neighborStart:neighborEnd], start-neighborStart, end-neighborStart)
		addReviewContextPart(&parts, &used, "Nearby text outside this analysis window:\n"+neighbor, contractReviewRelatedContextMaxRunes)
	}
	return strings.Join(parts, "")
}

func buildReviewClauses(reviewID, content string) []*types.ContractReviewClause {
	cfg := chunker.DefaultConfig()
	cfg.Strategy = chunker.StrategyAuto
	cfg.ChunkSize = contractReviewClauseChunkSize
	cfg.ChunkOverlap = contractReviewClauseChunkOverlap
	parts := chunker.Split(content, cfg)
	sourceHash := contractReviewSourceHash(content)
	rows := make([]*types.ContractReviewClause, 0, len(parts))
	for idx, part := range parts {
		title := fmt.Sprintf("Analysis segment %d", idx+1)
		if match := markdownHeading.FindStringSubmatch(part.Content); len(match) > 1 {
			title = strings.TrimSpace(match[1])
		}
		excerpt := strings.TrimSpace(part.Content)
		if len([]rune(excerpt)) > 360 {
			excerpt = string([]rune(excerpt)[:360]) + "…"
		}
		rows = append(rows, &types.ContractReviewClause{ReviewID: reviewID, Sequence: idx, Title: title, Excerpt: excerpt, SourceStart: part.Start, SourceEnd: part.End, EvidenceID: contractReviewEvidenceID(sourceHash, part.Start, part.End)})
	}
	return rows
}

func parseModelJSON(content string, target any) error {
	return decodeContractReviewJSON(content, target)
}

func (s *contractReviewService) resolveReviewModel(ctx context.Context, review *types.ContractReview) (chat.Chat, *types.CustomAgent, error) {
	modelCtx := ctx
	if review != nil {
		modelCtx = contractReviewModelContext(ctx, review.TenantID, review.UserID)
	}
	var agent *types.CustomAgent
	if s.agents != nil {
		agent, _ = s.agents.GetAgentByID(modelCtx, types.BuiltinContractReviewID)
	}
	modelID := ""
	if review != nil {
		modelID = strings.TrimSpace(review.ModelID)
	}
	if modelID != "" {
		if review == nil {
			return nil, agent, ErrContractReviewInvalidModel
		}
		if err := s.validateReviewModel(modelCtx, review.TenantID, review.UserID, modelID); err != nil {
			return nil, agent, err
		}
		model, err := s.models.GetChatModel(modelCtx, modelID)
		return model, agent, err
	}
	if agent != nil {
		modelID = strings.TrimSpace(agent.Config.ModelID)
	}
	if modelID == "" {
		if s.models == nil {
			return nil, agent, ErrContractReviewModelMissing
		}
		models, err := s.models.ListModels(modelCtx)
		if err != nil {
			return nil, agent, err
		}
		for _, m := range models {
			if m != nil && m.Type == types.ModelTypeKnowledgeQA && m.Status == types.ModelStatusActive && m.IsDefault {
				modelID = m.ID
				break
			}
		}
		if modelID == "" {
			for _, m := range models {
				if m != nil && m.Type == types.ModelTypeKnowledgeQA && m.Status == types.ModelStatusActive {
					modelID = m.ID
					break
				}
			}
		}
	}
	if modelID == "" {
		return nil, agent, ErrContractReviewModelMissing
	}
	model, err := s.models.GetChatModel(modelCtx, modelID)
	return model, agent, err
}

func validRisk(value string) types.ContractReviewRiskLevel {
	risk, err := normalizeContractReviewRisk(value)
	if err != nil {
		// Do not turn an invalid model value into a valid-looking risk level.
		// Strict batch validation rejects this before an issue is persisted.
		return ""
	}
	return risk
}

func contractReviewClauseMaxCompletionTokens(agent *types.CustomAgent) int {
	if agent != nil && agent.Config.MaxCompletionTokens > 0 {
		return agent.Config.MaxCompletionTokens
	}
	return contractReviewClauseDefaultMaxCompletionTokens
}

func contractReviewClauseRetryTokens(current int) int {
	if current < contractReviewClauseRetryMaxCompletionTokens {
		return contractReviewClauseRetryMaxCompletionTokens
	}
	return current
}

func contractReviewOutputReachedLimit(resp *types.ChatResponse) bool {
	if resp == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(resp.FinishReason)) {
	case "length", "max_tokens", "max_completion_tokens":
		return true
	default:
		return false
	}
}

func issueFingerprint(reviewID, clauseID, title, quote string) string {
	sum := sha256.Sum256([]byte(reviewID + "\x00" + clauseID + "\x00" + strings.ToLower(strings.TrimSpace(title)) + "\x00" + strings.Join(strings.Fields(quote), " ")))
	return hex.EncodeToString(sum[:])
}

// normalizeReviewQuoteText removes formatting whitespace while preserving the
// text that carries legal meaning. Document readers commonly insert line
// breaks or spaces inside a sentence, while the model returns the same quote
// as one line.
func normalizeReviewQuoteText(value string) string {
	var normalized strings.Builder
	normalized.Grow(len(value))
	for _, r := range value {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' {
			continue
		}
		normalized.WriteRune(r)
	}
	return normalized.String()
}

func reviewTitleBigrams(value string) map[string]struct{} {
	runes := []rune(normalizeReviewQuoteText(value))
	grams := make(map[string]struct{})
	for index := 0; index+1 < len(runes); index++ {
		if !unicode.IsLetter(runes[index]) && !unicode.IsDigit(runes[index]) {
			continue
		}
		if !unicode.IsLetter(runes[index+1]) && !unicode.IsDigit(runes[index+1]) {
			continue
		}
		grams[string(runes[index:index+2])] = struct{}{}
	}
	return grams
}

func reviewIssuesAreDuplicate(left, right reviewIssueOutput) bool {
	leftQuote := normalizeReviewQuoteText(left.OriginalQuote)
	rightQuote := normalizeReviewQuoteText(right.OriginalQuote)
	if len([]rune(leftQuote)) < 20 || len([]rune(rightQuote)) < 20 {
		return false
	}
	if !strings.Contains(leftQuote, rightQuote) && !strings.Contains(rightQuote, leftQuote) {
		return false
	}

	leftTerms, rightTerms := reviewTitleBigrams(left.Title), reviewTitleBigrams(right.Title)
	common := 0
	for term := range leftTerms {
		if _, ok := rightTerms[term]; ok {
			common++
		}
	}
	return common >= 2
}

func reviewIssueAlreadyAccepted(candidate reviewIssueOutput, accepted []reviewIssueOutput) bool {
	for _, previous := range accepted {
		if reviewIssuesAreDuplicate(candidate, previous) {
			return true
		}
	}
	return false
}

func (s *contractReviewService) ProcessReview(ctx context.Context, task *asynq.Task) error {
	var p types.ContractReviewTaskPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return err
	}
	ctx = reviewTaskContext(ctx, p)
	r, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
	if err != nil {
		if errors.Is(err, ErrContractReviewNotFound) {
			return nil
		}
		return err
	}
	// Analysis tasks must carry both the run id and the configuration snapshot.
	// Older queued tasks are intentionally ignored instead of being allowed to
	// write results into a newer run.
	if strings.TrimSpace(p.AnalysisRunID) == "" || strings.TrimSpace(p.ConfigHash) == "" ||
		!contractReviewTaskMatchesRun(p, r) || p.ConfigHash != r.ConfigHash {
		return nil
	}
	if r.Status != types.ContractReviewStatusAnalyzing && r.Status != types.ContractReviewStatusReviewingClauses {
		return nil
	}
	runID := r.AnalysisRunID
	if r.SourceTextHash == "" && r.SourceHash == "" {
		r.SourceHash = contractReviewSourceHash(r.ExtractedContent)
		r.SourceTextHash = r.SourceHash
		r.SourceRevision = "text-v2:" + r.SourceTextHash
		locator, locatorErr := buildContractReviewLocator(r.SourceTextHash, r.ExtractedContent, nil)
		if locatorErr != nil {
			return s.fail(ctx, r, locatorErr)
		}
		r.Locator = locator
		r.ConfigHash = contractReviewConfigHash(r)
		if err := s.updateReviewForRun(ctx, r, runID); err != nil {
			if errors.Is(err, errContractReviewStaleRun) {
				return nil
			}
			return err
		}
	}
	if r.SourceTextHash == "" {
		r.SourceTextHash = r.SourceHash
	}
	if r.SourceRevision == "" && r.SourceTextHash != "" {
		r.SourceRevision = "text-v2:" + r.SourceTextHash
	}
	model, agent, err := s.resolveReviewModel(ctx, r)
	if err != nil {
		return s.fail(ctx, r, err)
	}
	clauses := buildReviewClauses(r.ID, r.ExtractedContent)
	if len(clauses) == 0 {
		return s.fail(ctx, r, errors.New("no reviewable clauses found"))
	}
	if err := s.repo.ReplaceClausesForRun(ctx, r.ID, runID, clauses); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return s.fail(ctx, r, err)
	}
	r.Status, r.Progress = types.ContractReviewStatusReviewingClauses, 25
	if err := s.updateReviewForRun(ctx, r, runID); err != nil {
		if errors.Is(err, errContractReviewStaleRun) {
			return nil
		}
		return err
	}
	// The built-in contract-review agent has a user-facing full-report prompt
	// (headings, citations, and seven report sections). Do not prepend it here:
	// this worker needs a small machine-readable clause result, and mixing the
	// two contracts is what makes the model produce verbose/truncated output.
	systemPrompt := contractReviewClauseSystemPrompt
	maxTokens, temperature := contractReviewClauseMaxCompletionTokens(agent), 0.2
	if agent != nil {
		temperature = agent.Config.Temperature
	}
	thinking := false
	format := json.RawMessage(`{"type":"object","properties":{"issues":{"type":"array","maxItems":5,"items":{"type":"object","properties":{"category":{"type":"string","enum":["scope","parties","payment","term","acceptance","liability","dispute_resolution","guarantee","confidentiality","intellectual_property","data_security","compliance","other"]},"finding_type":{"type":"string","enum":["missing","contradiction","ambiguity","placeholder","external_reference","inconsistency"]},"risk_level":{"type":"string","enum":["high","medium","low"]},"title":{"type":"string","maxLength":80},"explanation":{"type":"string","maxLength":540},"original_quote":{"type":"string","maxLength":360},"suggestion":{"type":"string","maxLength":540},"evidence_refs":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"string"}}},"required":["category","finding_type","risk_level","title","explanation","original_quote","suggestion","evidence_refs"],"additionalProperties":false}},"facts":{"type":"array","maxItems":12,"items":{"type":"object","properties":{"type":{"type":"string","enum":["party","date","term","amount","payment","deposit","acceptance","dispute","placeholder","reference"]},"key":{"type":"string","maxLength":80},"value":{"type":"string","maxLength":540},"normalized_value":{"type":"string","maxLength":160},"unit":{"type":"string","maxLength":40},"currency":{"type":"string","maxLength":16},"condition":{"type":"string","maxLength":180},"evidence_quote":{"type":"string","maxLength":360},"evidence_refs":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"string"}}},"required":["type","key","value","evidence_quote","evidence_refs"],"additionalProperties":false}}},"required":["issues","facts"],"additionalProperties":false}`)
	issueSeq := 0
	// Facts are extracted once from the complete source after all clause
	// windows finish. Keep the legacy facts shape optional for compatibility
	// with older responses, but do not require it in the clause-stage contract.
	format = json.RawMessage(strings.Replace(string(format), `"maxItems":12`, `"maxItems":0`, 1))
	format = json.RawMessage(strings.Replace(string(format), `"required":["issues","facts"]`, `"required":["issues"]`, 1))
	acceptedReviewIssues := make([]reviewIssueOutput, 0)
	modelIssueWarnings := make([]types.ContractReviewWarning, 0)
	allFacts := make([]types.ContractReviewFact, 0)
	contentRunes := []rune(r.ExtractedContent)
	documentContext := newReviewDocumentContext(r.ExtractedContent)
	for idx, clause := range clauses {
		latest, getErr := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
		if getErr != nil {
			if errors.Is(getErr, ErrContractReviewNotFound) {
				return nil
			}
			return getErr
		}
		if !contractReviewTaskMatchesRun(p, latest) {
			return nil
		}
		if latest.DeletedAt.Valid {
			return nil
		}
		if clause.SourceStart < 0 || clause.SourceEnd > len(contentRunes) || clause.SourceStart >= clause.SourceEnd {
			return s.fail(ctx, r, fmt.Errorf("invalid source range for clause %d", idx+1))
		}
		units := reviewPromptEvidenceUnits(firstNonEmptyReviewString(r.SourceTextHash, r.SourceHash), clause, r.ExtractedContent, documentContext)
		if len(units) == 0 {
			return s.fail(ctx, r, fmt.Errorf("no evidence units found for clause %d", idx+1))
		}
		playbook, _ := contractReviewPlaybook(r.PlaybookID)
		prompt := fmt.Sprintf("Playbook: %s v%s\nRepresented party: %s\nAnalysis-window title: %s\nPrimary evidence window_id=%s. Prefer this ID for issues about the current window. A supporting evidence ID may be used only when the exact quoted passage is in that supporting unit and it establishes coverage, contradiction, or a related requirement. Every issue must cite one or more exact evidence units. The service extracts facts from the full source after all clause windows finish; do not return a facts field. Copy original_quote as a contiguous exact passage from the cited unit; do not paraphrase, combine unrelated passages, or invent values. Include a nearby heading, clause number, or other distinctive context so each passage identifies one location when a short phrase is repeated. If an exact unique passage is not available, omit that issue.\nEvidence units:\n%s", playbook.Name, r.PlaybookVersion, r.RepresentedParty, clause.Title, clause.EvidenceID, renderReviewEvidencePrompt(units))
		var validated reviewValidatedBatch
		var callErr error
		lastBatchResponse := ""
		for attempt := 0; attempt < 2; attempt++ {
			attemptPrompt := prompt
			attemptMaxTokens := maxTokens
			if attempt > 0 {
				attemptMaxTokens = contractReviewClauseRetryTokens(maxTokens)
				recoveryHint := "Use a contiguous exact passage that is unique in the cited source units; if that cannot be done, omit the item."
				if strings.Contains(callErr.Error(), "ambiguous") {
					recoveryHint = "The previous quotation was ambiguous. Replace it with a longer, distinctive contiguous exact passage containing a nearby heading or clause number; if no unique passage is available, omit the item."
				} else if strings.Contains(callErr.Error(), "cannot be located") {
					recoveryHint = "The previous quotation could not be located. Copy the passage character-for-character from one cited evidence unit, or omit the item."
				}
				attemptPrompt += fmt.Sprintf("\nRecovery instruction: the previous response failed validation (%s). Prioritize a valid compact response over completeness: return no more than three issues for this window, and omit any issue whose exact quote is uncertain. %s Output only the compact {\"issues\": [...]} object now; do not add any facts field, explanation, report headings, citations, or markdown.", callErr, recoveryHint)
			}
			resp, e := model.Chat(ctx, []chat.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: attemptPrompt}}, &chat.ChatOptions{Temperature: temperature, MaxCompletionTokens: attemptMaxTokens, Thinking: &thinking, Format: format})
			if e != nil {
				callErr = e
				continue
			}
			if resp == nil {
				callErr = errors.New("model returned no response")
				continue
			}
			lastBatchResponse = resp.Content
			if contractReviewOutputReachedLimit(resp) {
				callErr = fmt.Errorf("model output reached the %d-token completion limit", attemptMaxTokens)
				continue
			}
			validated, callErr = validateReviewBatchJSON(resp.Content, r.ExtractedContent, units)
			if callErr == nil {
				break
			}
		}
		if callErr != nil {
			// A malformed model quote is isolated only after both normal attempts
			// fail. The quote remains unusable and is never persisted as evidence;
			// unrelated validated issues can still produce a useful, explicitly
			// degraded review result.
			if strings.TrimSpace(lastBatchResponse) != "" {
				partial, failures, partialErr := validateReviewBatchWithEvidenceIsolation(lastBatchResponse, r.ExtractedContent, units)
				if partialErr == nil && len(failures) > 0 {
					validated = partial
					callErr = nil
					for _, failure := range failures {
						modelIssueWarnings = append(modelIssueWarnings, types.ContractReviewWarning{
							Code:       "MODEL_ISSUE_EVIDENCE_UNLOCATED",
							Message:    fmt.Sprintf("第 %d 条模型审查问题的原文证据无法可靠定位，已忽略该条；请以已定位的问题为准", failure.Index),
							ClauseID:   clause.ID,
							EvidenceID: clause.EvidenceID,
						})
					}
				}
			}
			if callErr != nil {
				return s.fail(ctx, r, fmt.Errorf("review clause %d: %w", idx+1, callErr))
			}
		}
		if validated.SkippedExcessIssues > 0 {
			modelIssueWarnings = append(modelIssueWarnings, types.ContractReviewWarning{
				Code:       "MODEL_ISSUE_LIMIT_EXCEEDED",
				Message:    fmt.Sprintf("第 %d 个分析片段中模型返回的问题超过单片段上限 %d，已忽略超出的 %d 条；请以保留的问题为准", idx+1, contractReviewMaxIssues, validated.SkippedExcessIssues),
				ClauseID:   clause.ID,
				EvidenceID: clause.EvidenceID,
			})
		}
		if validated.SkippedDuplicateIssues > 0 {
			modelIssueWarnings = append(modelIssueWarnings, types.ContractReviewWarning{
				Code:       "MODEL_ISSUE_DUPLICATE",
				Message:    fmt.Sprintf("第 %d 个分析片段中模型返回 %d 条重复问题，已忽略重复项；请以保留的问题为准", idx+1, validated.SkippedDuplicateIssues),
				ClauseID:   clause.ID,
				EvidenceID: clause.EvidenceID,
			})
		}
		latest, getErr = s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
		if getErr != nil {
			if errors.Is(getErr, ErrContractReviewNotFound) {
				return nil
			}
			return getErr
		}
		if !contractReviewTaskMatchesRun(p, latest) {
			return nil
		}
		for _, result := range validated.Issues {
			if reviewIssueAlreadyAcceptedAtEvidence(result, acceptedReviewIssues) {
				continue
			}
			evidenceRefs, marshalErr := json.Marshal(result.EvidenceRefs)
			if marshalErr != nil {
				return s.fail(ctx, r, fmt.Errorf("marshal issue evidence refs: %w", marshalErr))
			}
			issue := &types.ContractReviewIssue{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(issueFingerprint(r.ID, clause.ID, result.Title, result.OriginalQuote))).String(), ReviewID: r.ID, ClauseID: clause.ID,
				Fingerprint: issueFingerprint(r.ID, clause.ID, result.Title, result.OriginalQuote), Sequence: issueSeq, RiskLevel: validRisk(result.RiskLevel), Category: result.Category, FindingType: result.FindingType, Title: result.Title,
				Explanation: result.Explanation, OriginalQuote: result.canonicalQuote, Suggestion: result.Suggestion, EvidenceRefs: types.JSON(evidenceRefs), SourceStart: result.resolvedStart, SourceEnd: result.resolvedEnd}
			if err := s.repo.UpsertIssueForRun(ctx, issue, runID); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return s.fail(ctx, r, err)
			}
			acceptedReviewIssues = append(acceptedReviewIssues, result)
			issueSeq++
			clause.IssueCount++
		}
		if len(validated.Facts) > 0 {
			return s.fail(ctx, r, fmt.Errorf("review clause %d: model returned facts although fact extraction is service-owned", idx+1))
		}
		clause.ReviewStatus = "completed"
		if err := s.repo.UpdateClauseForRun(ctx, clause, runID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return s.fail(ctx, r, fmt.Errorf("persist clause %d result: %w", idx+1, err))
		}
		r.Progress = 25 + int(float64(idx+1)/float64(len(clauses))*60)
		if err := s.updateReviewForRun(ctx, r, runID); err != nil {
			if errors.Is(err, errContractReviewStaleRun) {
				return nil
			}
			return err
		}
	}
	allFacts = extractContractReviewFacts(r.ExtractedContent, firstNonEmptyReviewString(r.SourceTextHash, r.SourceHash))
	warnings := append([]types.ContractReviewWarning{}, modelIssueWarnings...)
	warnings = append(warnings, reviewFactConsistencyWarnings(allFacts)...)
	structuralWarnings := reviewDocumentStructureWarnings(r.ExtractedContent, firstNonEmptyReviewString(r.SourceTextHash, r.SourceHash))
	warnings = append(warnings, structuralWarnings...)
	if len(contractReviewLocatorUnits(r.Locator)) == 0 {
		warnings = append(warnings, types.ContractReviewWarning{Code: "LOCATOR_UNSUPPORTED", Message: "解析器未提供可验证的文档定位单元，当前结果暂不支持精确定位"})
	}
	if len(partiesFromReviewFacts(allFacts)) == 0 {
		warnings = append(warnings, types.ContractReviewWarning{Code: "PARTIES_NOT_IDENTIFIED", Message: "未能从合同原文中识别到有证据支持的当事方"})
	}
	warnings = dedupeContractReviewWarnings(warnings)
	detail, err := s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
	if err != nil {
		return err
	}
	// Deterministic source-structure findings (blank/placeholder fields,
	// unchecked dispute options, and external references) are materialized as
	// ordinary issues. This keeps them visible in the same evidence workflow as
	// model findings while their risk level remains owned by this rule set.
	derivedSequence := len(detail.Issues)
	for _, warning := range structuralWarnings {
		category := contractReviewWarningCategory(r.ExtractedContent, warning)
		findingType := contractReviewWarningFindingType(warning.Code)
		duplicate := false
		for _, existing := range detail.Issues {
			if contractReviewIssueCoversWarning(existing, warning, category, findingType) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		issue, clause, buildErr := buildContractReviewStructureIssue(r.ID, firstNonEmptyReviewString(r.SourceTextHash, r.SourceHash), r.ExtractedContent, warning, clauses, derivedSequence)
		if buildErr != nil {
			return s.fail(ctx, r, fmt.Errorf("build structural contract issue: %w", buildErr))
		}
		if err := s.repo.UpsertIssueForRun(ctx, issue, runID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return s.fail(ctx, r, fmt.Errorf("persist structural contract issue: %w", err))
		}
		for warningIndex := range warnings {
			if warnings[warningIndex].EvidenceID == warning.EvidenceID {
				warnings[warningIndex].ClauseID = clause.ID
			}
		}
		derivedSequence++
	}
	if len(structuralWarnings) > 0 {
		// Re-read after deterministic issue insertion so the derived counts and
		// recommendation list include both model and rule-based findings.
		detail, err = s.Get(ctx, p.TenantID, p.UserID, p.ReviewID)
		if err != nil {
			return err
		}
	}
	aggregate := aggregateContractReviewRisks(detail.Issues)
	issueSummaries := make([]map[string]any, 0, len(detail.Issues))
	for _, issue := range detail.Issues {
		if issue == nil {
			continue
		}
		issueSummaries = append(issueSummaries, map[string]any{"id": issue.ID, "risk_level": issue.RiskLevel, "category": issue.Category, "finding_type": issue.FindingType, "title": issue.Title, "explanation": issue.Explanation, "suggestion": issue.Suggestion, "evidence_refs": issue.EvidenceRefs})
	}
	factJSON, marshalErr := json.Marshal(allFacts)
	if marshalErr != nil {
		return s.fail(ctx, r, fmt.Errorf("marshal contract review facts: %w", marshalErr))
	}
	issueJSON, marshalErr := json.Marshal(issueSummaries)
	if marshalErr != nil {
		return s.fail(ctx, r, fmt.Errorf("marshal contract review issue summaries: %w", marshalErr))
	}
	overviewPrompt := fmt.Sprintf("请用简体中文总结本次合同审查并返回 JSON。合同文件名：%s。当前代表方：%s。以下是服务端已验证的问题和事实，只能依据这些内容总结。问题 JSON：%s\n事实 JSON：%s\n最终风险数量由服务端计算为：高=%d，中=%d，低=%d。", r.FileName, r.RepresentedParty, issueJSON, factJSON, aggregate.Counts["high"], aggregate.Counts["medium"], aggregate.Counts["low"])
	overviewFormat := json.RawMessage(`{"type":"object","properties":{"executive_summary":{"type":"string","maxLength":540},"contract_type":{"type":"string","maxLength":80},"parties":{"type":"array","items":{"type":"string","maxLength":80}},"key_recommendations":{"type":"array","items":{"type":"string","maxLength":540}}},"required":["executive_summary","contract_type","parties","key_recommendations"],"additionalProperties":false}`)
	var overview reviewOverviewOutput
	var overviewErr error
	for attempt := 0; attempt < 2; attempt++ {
		resp, callErr := model.Chat(ctx, []chat.Message{{Role: "system", Content: contractReviewOverviewSystemPrompt}, {Role: "user", Content: overviewPrompt}}, &chat.ChatOptions{Temperature: 0.1, MaxCompletionTokens: 1200, Thinking: &thinking, Format: overviewFormat})
		if callErr != nil {
			overviewErr = callErr
			continue
		}
		if resp == nil {
			overviewErr = errors.New("overview model returned no response")
			continue
		}
		if contractReviewOutputReachedLimit(resp) {
			overviewErr = errors.New("overview model output reached the completion limit")
			continue
		}
		overview, overviewErr = validateReviewOverviewJSON(resp.Content)
		if overviewErr == nil {
			break
		}
	}
	if overviewErr != nil {
		warnings = append(warnings, types.ContractReviewWarning{Code: "OVERVIEW_VALIDATION_FAILED", Message: overviewErr.Error()})
		fallback := reviewOverviewOutput{ExecutiveSummary: "条款审查已完成，但概览生成失败，请查看问题明细。", ContractType: "unknown", Parties: partiesFromReviewFacts(allFacts), KeyRecommendations: []string{}}
		fallbackJSON, fallbackErr := buildContractReviewOverviewJSON(fallback, detail.Issues, allFacts, types.ContractReviewQualityInvalid, warnings)
		if fallbackErr != nil {
			return s.fail(ctx, r, fmt.Errorf("overview validation failed: %w; build fallback failed: %v", overviewErr, fallbackErr))
		}
		r.Overview, r.Warnings = fallbackJSON, mustContractReviewWarningsJSON(warnings)
		r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusFailed, 100, fmt.Sprintf("overview validation failed: %v", overviewErr)
		r.QualityStatus = types.ContractReviewQualityInvalid
		if persistErr := s.updateReviewForRun(ctx, r, runID); persistErr != nil {
			if errors.Is(persistErr, errContractReviewStaleRun) {
				return nil
			}
			return fmt.Errorf("%w (also failed to persist overview failure: %v)", overviewErr, persistErr)
		}
		return overviewErr
	}
	quality := types.ContractReviewQualityValid
	if len(warnings) > 0 {
		quality = types.ContractReviewQualityDegraded
	}
	overviewJSON, err := buildContractReviewOverviewJSON(overview, detail.Issues, allFacts, quality, warnings)
	if err != nil {
		return s.fail(ctx, r, fmt.Errorf("build contract review overview: %w", err))
	}
	warningsJSON, err := contractReviewWarningJSON(warnings)
	if err != nil {
		return s.fail(ctx, r, fmt.Errorf("marshal contract review warnings: %w", err))
	}
	r.Overview, r.Warnings = overviewJSON, warningsJSON
	r.Status, r.Progress, r.ErrorMessage = types.ContractReviewStatusCompleted, 100, ""
	r.QualityStatus = quality
	now := time.Now()
	r.CompletedAt = &now
	if err := s.updateReviewForRun(ctx, r, runID); err != nil {
		if errors.Is(err, errContractReviewStaleRun) {
			return nil
		}
		logger.ErrorWithFields(ctx, err, nil)
		return err
	}
	return nil
}

func mustContractReviewWarningsJSON(warnings []types.ContractReviewWarning) types.JSON {
	encoded, err := contractReviewWarningJSON(warnings)
	if err != nil {
		return types.JSON(`[]`)
	}
	return encoded
}
