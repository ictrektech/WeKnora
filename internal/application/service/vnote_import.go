package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrVNoteImportBusy = errors.New("another V-Note note is being indexed")

// This error is returned only before staging/enqueueing the requested note.
// Clients may retry after fixing the model configuration without resolving an
// ambiguous processing attempt.
var ErrVNoteEmbeddingUnavailable = errors.New("configure an active default embedding model before synchronizing V-Note")

var vnoteEmbeddedResource = regexp.MustCompile(`(?i)!\[|<(?:img|video|audio|iframe|object|embed|script)\b|(?:resource|storage|file)://|/api/v1/resources/`)
var vnoteSourceVersion = regexp.MustCompile(`^[0-9a-f]{64}$`)

type VNoteImportResult struct {
	Accepted        bool   `json:"accepted"`
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	KnowledgeID     string `json:"knowledgeId"`
	State           string `json:"state"`
	Attempt         string `json:"attempt,omitempty"`
}

type VNoteImportService struct {
	database       *gorm.DB
	knowledgeBases interfaces.KnowledgeBaseService
	knowledge      interfaces.KnowledgeService
	models         interfaces.ModelService
}

func NewVNoteImportService(database *gorm.DB, knowledgeBases interfaces.KnowledgeBaseService, knowledge interfaces.KnowledgeService, models interfaces.ModelService) *VNoteImportService {
	return &VNoteImportService{database: database, knowledgeBases: knowledgeBases, knowledge: knowledge, models: models}
}

func (s *VNoteImportService) ApplyControl(ctx context.Context, subject, workspaceID string, version int64, enabled bool, roots []string) error {
	caller := types.CallerFromContext(ctx)
	if caller.UserID == "" || caller.TenantID == 0 || caller.Role != types.TenantRoleOwner {
		return access.ErrForbidden
	}
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repository.ApplyVNoteControl(ctx, tx, caller.UserID, workspaceID, subject, version, enabled, roots); err != nil {
			return err
		}
		return repository.RetireRevokedVNotePending(ctx, tx)
	})
}

func (s *VNoteImportService) Import(ctx context.Context, snapshot types.VNoteSnapshot) (*VNoteImportResult, error) {
	caller := types.CallerFromContext(ctx)
	if caller.UserID == "" || caller.TenantID == 0 || !caller.Role.HasPermission(types.TenantRoleContributor) {
		return nil, access.ErrForbidden
	}
	if snapshot.Protocol != 1 || snapshot.Subject == "" || snapshot.WorkspaceID == "" || len(snapshot.WorkspaceID) > 128 || snapshot.ConfigVersion < 1 || snapshot.RootID == "" || len(snapshot.RootID) > 128 || snapshot.NoteID == "" || len(snapshot.NoteID) > 128 ||
		strings.TrimSpace(snapshot.Markdown) == "" || utf8.RuneCountInString(snapshot.Markdown) > 180000 || len(snapshot.Title) > 2000 {
		return nil, apperrors.NewBadRequestError("unsupported V-Note snapshot")
	}
	if vnoteEmbeddedResource.MatchString(snapshot.Title + "\n" + snapshot.Markdown) {
		return nil, apperrors.NewBadRequestError("this stage accepts prose Markdown without embedded resources")
	}
	allowed, err := repository.VNoteControlAllows(ctx, s.database, caller.UserID, snapshot.WorkspaceID, snapshot.Subject, snapshot.RootID, snapshot.ConfigVersion)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, access.ErrForbidden
	}
	if secret := types.VNoteDelegatedCredential(ctx); secret != "" {
		if !vnoteSourceVersion.MatchString(snapshot.SourceHash) {
			return nil, apperrors.NewBadRequestError("source version is required for background synchronization")
		}
		binding, err := s.VNoteDelegationBinding(ctx, secret)
		if err != nil || binding.UserID != caller.UserID || binding.WorkspaceID != snapshot.WorkspaceID || binding.Subject != snapshot.Subject {
			return nil, access.ErrForbidden
		}
	}
	kbID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-kb\x00"+caller.UserID+"\x00"+snapshot.RootID)).String()
	stateID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(kbID+"\x00"+snapshot.NoteID)).String()
	content := "# " + strings.ReplaceAll(strings.TrimSpace(snapshot.Title), "\n", " ") + "\n\n" + strings.TrimSpace(snapshot.Markdown) + "\n"
	hashBytes := sha256.Sum256([]byte(content))
	contentHash := hex.EncodeToString(hashBytes[:])
	state := types.VNoteImportState{ID: stateID, UserID: caller.UserID, KnowledgeBaseID: kbID}
	if err := s.database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
		return nil, err
	}
	if err := s.database.WithContext(ctx).Where("id = ? AND user_id = ?", stateID, caller.UserID).First(&state).Error; err != nil {
		return nil, err
	}
	if state.KnowledgeID != "" {
		existingKB, kbErr := s.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID)
		if kbErr != nil {
			return nil, kbErr
		}
		if existingKB.PrivateOwnerID != caller.UserID || existingKB.VNoteRootID != snapshot.RootID {
			return nil, access.ErrForbidden
		}
		if existingKB.Name != types.VNoteKnowledgeBaseName(snapshot.RootName) {
			if err := repository.RenameVNoteKnowledgeBase(ctx, s.database, vnoteRootScope(ctx, snapshot.WorkspaceID, snapshot.Subject, snapshot.RootID, snapshot.ConfigVersion), existingKB, snapshot.RootName); err != nil {
				return nil, err
			}
		}
		previous, err := s.knowledge.GetRepository().GetKnowledgeByID(ctx, caller.TenantID, state.KnowledgeID)
		if err != nil {
			return nil, err
		}
		if previous.KnowledgeBaseID != kbID {
			return nil, access.ErrForbidden
		}
		if state.ContentHash == contentHash {
			if previous.ParseStatus == types.ParseStatusCompleted {
				return &VNoteImportResult{Accepted: true, KnowledgeBaseID: kbID, KnowledgeID: previous.ID, State: previous.ParseStatus}, nil
			}
			meta := previous.GetMetadata()
			if (previous.ParseStatus == types.ParseStatusPending || previous.ParseStatus == types.ParseStatusProcessing || previous.ParseStatus == types.ParseStatusFinalizing) &&
				meta["vnote_workspace_id"] == snapshot.WorkspaceID && meta["vnote_subject"] == snapshot.Subject && meta["vnote_config_version"] == fmt.Sprint(snapshot.ConfigVersion) {
				return &VNoteImportResult{Accepted: true, KnowledgeBaseID: kbID, KnowledgeID: previous.ID, State: previous.ParseStatus, Attempt: meta["vnote_attempt"]}, nil
			}
		}
	}
	var staged types.VNoteImportGeneration
	lookup := s.database.WithContext(ctx).Table("vnote_import_generations AS gen").Select("gen.*").
		Joins("JOIN vnote_import_gates g ON g.id = ? AND g.knowledge_id = gen.knowledge_id AND g.attempt = gen.attempt", "index").
		Where("gen.state_id = ? AND gen.user_id = ? AND gen.workspace_id = ? AND gen.subject = ? AND gen.config_version = ? AND gen.content_hash = ? AND gen.status = ?",
			stateID, caller.UserID, snapshot.WorkspaceID, snapshot.Subject, snapshot.ConfigVersion, contentHash, "staged").First(&staged)
	if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return nil, lookup.Error
	}
	if lookup.Error == nil {
		if _, err := s.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID); err != nil {
			return nil, err
		}
		stageCtx := types.WithVNoteAdmission(ctx, staged.Attempt)
		previous, err := s.knowledge.GetRepository().GetKnowledgeByID(stageCtx, caller.TenantID, staged.KnowledgeID)
		if err == nil && (previous.ParseStatus == types.ParseStatusPending || previous.ParseStatus == types.ParseStatusProcessing || previous.ParseStatus == types.ParseStatusFinalizing) {
			return &VNoteImportResult{Accepted: true, KnowledgeBaseID: kbID, KnowledgeID: previous.ID, State: previous.ParseStatus, Attempt: staged.Attempt}, nil
		}
	}
	token, err := s.acquireGate(ctx)
	if err != nil {
		return nil, err
	}
	defer s.database.WithContext(context.WithoutCancel(ctx)).Model(&types.VNoteImportGate{}).Where("id = ? AND token = ?", "index", token).Update("lease_until", time.Now().UTC())
	gate := types.VNoteImportGate{}
	if err := s.database.WithContext(ctx).First(&gate, "id = ?", "index").Error; err != nil {
		return nil, err
	}
	if gate.KnowledgeID != "" {
		var active types.Knowledge
		lookupErr := s.database.WithContext(ctx).Model(&types.Knowledge{}).Select("parse_status").Where("tenant_id = ? AND id = ?", gate.TenantID, gate.KnowledgeID).First(&active).Error
		if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return nil, lookupErr
		}
		if lookupErr == nil && (active.ParseStatus == types.ParseStatusPending || active.ParseStatus == types.ParseStatusProcessing || active.ParseStatus == types.ParseStatusFinalizing) {
			return nil, ErrVNoteImportBusy
		}
	}
	kb, err := s.ensureVNoteKnowledgeBase(ctx, vnoteRootScope(ctx, snapshot.WorkspaceID, snapshot.Subject, snapshot.RootID, snapshot.ConfigVersion), snapshot.RootName)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.PrivateOwnerID != caller.UserID || kb.VNoteRootID != snapshot.RootID || kb.TenantID != caller.TenantID {
		return nil, access.ErrForbidden
	}
	writeContext, err := withVNoteMutation(ctx, kb)
	if err != nil {
		return nil, err
	}
	writeContext = types.WithVNoteAdmission(writeContext, token)
	metadata := map[string]string{"vnote_root_id": snapshot.RootID, "vnote_note_id": snapshot.NoteID, "vnote_workspace_id": snapshot.WorkspaceID, "vnote_subject": snapshot.Subject, "vnote_config_version": fmt.Sprint(snapshot.ConfigVersion), "source_updated_at": snapshot.UpdatedAt, "vnote_content_hash": contentHash, "channel": "vnote", "vnote_attempt": token}
	metadata["vnote_owner_id"] = caller.UserID
	metadata["vnote_source_hash"] = snapshot.SourceHash
	metadata["vnote_source_title"] = snapshot.Title
	if types.VNoteScheduledOperation(ctx) {
		metadata["vnote_scheduled"] = "true"
	}
	if types.VNoteDelegatedCredential(ctx) != "" {
		metadata["vnote_authorization"] = "delegated"
	}
	stageID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-generation\x00"+stateID+"\x00"+token)).String()
	generation := types.VNoteImportGeneration{Attempt: token, StateID: stateID, KnowledgeID: stageID, ExpectedKnowledgeID: state.KnowledgeID,
		UserID: caller.UserID, WorkspaceID: snapshot.WorkspaceID, Subject: snapshot.Subject, RootID: snapshot.RootID,
		ConfigVersion: snapshot.ConfigVersion, ContentHash: contentHash, Status: "staged"}
	err = s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claimed := tx.Model(&types.VNoteImportGate{}).Where("id = ? AND token = ? AND lease_until > ?", "index", token, time.Now().UTC()).
			Updates(map[string]any{"knowledge_id": stageID, "tenant_id": caller.TenantID, "content_hash": contentHash, "attempt": token})
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected != 1 {
			return ErrVNoteImportBusy
		}
		return tx.Create(&generation).Error
	})
	if err != nil {
		return nil, err
	}
	draft, err := s.knowledge.CreateKnowledgeFromManual(writeContext, kbID, &types.ManualKnowledgePayload{InternalID: stageID, Title: snapshot.Title, Content: content, Status: types.ManualKnowledgeStatusDraft, SourceMetadata: metadata}, "vnote")
	if err != nil {
		return nil, err
	}
	if draft == nil || draft.ID != stageID {
		return nil, errors.New("V-Note staging document mismatch")
	}
	current, err := s.knowledge.GetRepository().GetKnowledgeByID(writeContext, caller.TenantID, stageID)
	if err != nil {
		return nil, err
	}
	reservedMetadata, err := mergeKnowledgeMetadata(current.Metadata, metadata)
	if err != nil {
		return nil, err
	}
	err = s.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		claimed := transaction.Model(&types.VNoteImportGate{}).Where("id = ? AND token = ? AND knowledge_id = ? AND lease_until > ?", "index", token, stageID, time.Now().UTC()).Update("attempt", token)
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected != 1 {
			return ErrVNoteImportBusy
		}
		reserved := transaction.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ? AND parse_status = ?", stageID, caller.TenantID, types.ManualKnowledgeStatusDraft).Updates(map[string]any{"metadata": reservedMetadata, "parse_status": types.ManualKnowledgeStatusDraft, "enable_status": "disabled"})
		if reserved.Error != nil {
			return reserved.Error
		}
		if reserved.RowsAffected != 1 {
			return ErrVNoteImportBusy
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	off := false
	payload := &types.ManualKnowledgePayload{Title: snapshot.Title, Content: content, Status: types.ManualKnowledgeStatusPublish, SourceMetadata: metadata,
		ProcessConfig: &types.KnowledgeProcessOverrides{SummaryEnabled: &off, EnableMultimodel: &off, GraphEnabled: &off, ImageAttrsEnabled: &off,
			QuestionGenerationConfig: &types.QuestionGenerationConfig{Enabled: false}}}
	document, err := s.knowledge.UpdateManualKnowledge(writeContext, stageID, payload)
	if err != nil {
		return nil, err
	}
	if document == nil || document.ParseStatus == types.ParseStatusFailed {
		return nil, errors.New("V-Note document was not admitted for processing")
	}
	return &VNoteImportResult{Accepted: true, KnowledgeBaseID: kbID, KnowledgeID: document.ID, State: document.ParseStatus, Attempt: token}, nil
}

func (s *VNoteImportService) acquireGate(ctx context.Context) (string, error) {
	now := time.Now().UTC()
	if err := s.database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&types.VNoteImportGate{ID: "index", LeaseUntil: now.Add(-time.Second)}).Error; err != nil {
		return "", err
	}
	token := uuid.NewString()
	result := s.database.WithContext(ctx).Model(&types.VNoteImportGate{}).Where("id = ? AND lease_until <= ?", "index", now).Updates(map[string]any{"token": token, "lease_until": now.Add(time.Minute)})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", ErrVNoteImportBusy
	}
	return token, nil
}

func (s *VNoteImportService) defaultEmbedding(ctx context.Context) (string, error) {
	models, err := s.models.ListModels(ctx)
	if err != nil {
		return "", err
	}
	// ListModels overlays workspace defaults on deployment defaults. Reuse the
	// shared precedence, but do not let its first-model fallback silently choose
	// a user-created provider for unattended indexing.
	var defaults, builtins []*types.Model
	for _, model := range models {
		if model == nil || model.ID == "" || model.Type != types.ModelTypeEmbedding || model.Status != types.ModelStatusActive {
			continue
		}
		if model.IsDefault || slices.Contains(model.DefaultFor, string(types.ModelTypeEmbedding)) {
			defaults = append(defaults, model)
		}
		if model.IsBuiltin && model.ManagedBy == "yaml" {
			builtins = append(builtins, model)
		}
	}
	if model := types.PickDefaultModel(defaults, types.ModelTypeEmbedding); model != nil {
		return model.ID, nil
	}
	if len(builtins) == 1 {
		return builtins[0].ID, nil
	}
	return "", ErrVNoteEmbeddingUnavailable
}

// Status reports only the caller's own managed note, without returning its content.
func (s *VNoteImportService) Status(ctx context.Context, rootID, noteID string) (*VNoteImportResult, error) {
	caller := types.CallerFromContext(ctx)
	if caller.UserID == "" || caller.TenantID == 0 {
		return nil, access.ErrForbidden
	}
	kbID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-kb\x00"+caller.UserID+"\x00"+rootID)).String()
	stateID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(kbID+"\x00"+noteID)).String()
	var state types.VNoteImportState
	err := s.database.WithContext(ctx).Where("id = ? AND user_id = ? AND knowledge_base_id = ?", stateID, caller.UserID, kbID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &VNoteImportResult{State: "not_imported"}, nil
	}
	if err != nil {
		return nil, err
	}
	var active types.VNoteImportGeneration
	activeErr := s.database.WithContext(ctx).Table("vnote_import_generations AS gen").Select("gen.*").
		Joins("JOIN vnote_import_gates g ON g.id = ? AND g.knowledge_id = gen.knowledge_id AND g.attempt = gen.attempt", "index").
		Where("gen.state_id = ? AND gen.user_id = ? AND gen.status = ?", stateID, caller.UserID, "staged").First(&active).Error
	if activeErr != nil && !errors.Is(activeErr, gorm.ErrRecordNotFound) {
		return nil, activeErr
	}
	if state.KnowledgeID == "" && errors.Is(activeErr, gorm.ErrRecordNotFound) {
		return &VNoteImportResult{State: "not_imported"}, nil
	}
	kb, err := s.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if kb.PrivateOwnerID != caller.UserID || kb.TenantID != caller.TenantID || kb.VNoteRootID != rootID {
		return nil, access.ErrForbidden
	}
	physicalID := state.KnowledgeID
	readCtx := ctx
	if activeErr == nil {
		physicalID = active.KnowledgeID
		readCtx = types.WithVNoteAdmission(ctx, active.Attempt)
	}
	note, err := s.knowledge.GetRepository().GetKnowledgeByID(readCtx, caller.TenantID, physicalID)
	if err != nil {
		return nil, err
	}
	if note.KnowledgeBaseID != kbID || note.Channel != "vnote" {
		return nil, access.ErrForbidden
	}
	stateName := note.ParseStatus
	if stateName == types.ParseStatusProcessing && note.ErrorMessage != "" {
		stateName = "recovery_required"
	}
	return &VNoteImportResult{Accepted: true, KnowledgeBaseID: kbID, KnowledgeID: note.ID, State: stateName, Attempt: note.GetMetadata()["vnote_attempt"]}, nil
}
