package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// V-Note bodies are inline metadata. Guard direct document reads too, including
// callers that only hold a persisted document-to-KB reference.
func (r *knowledgeRepository) canLoadVNoteKnowledge(ctx context.Context, knowledge *types.Knowledge) (bool, error) {
	if knowledge.Channel != "vnote" {
		return true, nil
	}
	_, err := (&knowledgeBaseRepository{db: r.db}).GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
	if errors.Is(err, ErrKnowledgeBaseNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	hidden, err := r.HiddenVNoteGenerationIDs(ctx, []string{knowledge.ID})
	if err != nil {
		return false, err
	}
	if !hidden[knowledge.ID] {
		return true, nil
	}
	attempt := types.VNoteAdmissionFromContext(ctx)
	if attempt == "" {
		return false, nil
	}
	var count int64
	err = r.db.WithContext(ctx).Model(&types.VNoteImportGate{}).
		Where("id = ? AND knowledge_id = ? AND attempt = ?", "index", knowledge.ID, attempt).Count(&count).Error
	return err == nil && count == 1, err
}

func (r *knowledgeRepository) filterVNoteKnowledge(ctx context.Context, rows []*types.Knowledge) ([]*types.Knowledge, error) {
	visible := make([]*types.Knowledge, 0, len(rows))
	for _, row := range rows {
		if row.Channel != "vnote" {
			visible = append(visible, row)
			continue
		}
		allowed, err := r.canLoadVNoteKnowledge(ctx, row)
		if err != nil {
			return nil, err
		}
		if allowed {
			visible = append(visible, row)
		}
	}
	return visible, nil
}

func (r *knowledgeRepository) updateVNoteKnowledge(ctx context.Context, knowledge *types.Knowledge, attempt string) error {
	extraction := "metadata->>'vnote_attempt'"
	if r.db.Dialector.Name() == "sqlite" {
		extraction = "json_extract(metadata, '$.vnote_attempt')"
	}
	if knowledge.ParseStatus == types.ParseStatusFailed {
		// A failed or timed-out model request does not prove the remote model
		// stopped. Keep the processing slot occupied until a recovery protocol
		// can establish that, but retain the error for diagnosis.
		// Attempt-fenced diagnostics may survive revocation; this branch never
		// writes content, metadata, status or the published pointer.
		held := r.db.WithContext(ctx).Model(&types.Knowledge{}).
			Where("id = ? AND tenant_id = ? AND parse_status = ?", knowledge.ID, knowledge.TenantID, types.ParseStatusProcessing).
			Where(extraction+" = ?", attempt).
			Where("EXISTS (SELECT 1 FROM vnote_import_gates g WHERE g.id = ? AND g.knowledge_id = ? AND g.attempt = ?)", "index", knowledge.ID, attempt).
			Updates(map[string]any{"error_message": knowledge.ErrorMessage, "updated_at": time.Now().UTC()})
		if held.Error != nil {
			return held.Error
		}
		if held.RowsAffected == 1 {
			return nil
		}
	}
	result := r.db.WithContext(ctx).Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ?", knowledge.ID, knowledge.TenantID).
		Where(extraction+" = ?", attempt).
		Where(r.vnoteControlPredicate()).
		Where("EXISTS (SELECT 1 FROM vnote_import_gates g WHERE g.id = ? AND g.knowledge_id = ? AND g.attempt = ?)", "index", knowledge.ID, attempt).
		Select("*").Omit(omitFieldsOnUpdate...).Updates(knowledge)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("V-Note submission was superseded")
	}
	return nil
}

func (r *knowledgeRepository) BeginVNoteProcessing(ctx context.Context, knowledgeID, contentHash, attempt string) (bool, error) {
	extraction := "metadata->>'vnote_attempt'"
	if r.db.Dialector.Name() == "sqlite" {
		extraction = "json_extract(metadata, '$.vnote_attempt')"
	}
	result := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("id = ? AND parse_status = ?", knowledgeID, types.ParseStatusPending).
		Where(extraction+" = ?", attempt).
		Where(r.vnoteControlPredicate()).
		Where("EXISTS (SELECT 1 FROM vnote_import_gates g WHERE g.id = ? AND g.knowledge_id = ? AND g.content_hash = ? AND g.attempt = ?)", "index", knowledgeID, contentHash, attempt).
		Updates(map[string]any{"parse_status": types.ParseStatusProcessing, "updated_at": time.Now().UTC()})
	if result.Error != nil || result.RowsAffected == 1 {
		return result.RowsAffected == 1, result.Error
	}
	// A queued task that lost authorization never started a model call. Retire
	// only that exact pending attempt; a concurrently running task keeps the
	// global slot until its worker has actually stopped.
	retired := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("id = ? AND parse_status = ?", knowledgeID, types.ParseStatusPending).
		Where(extraction+" = ?", attempt).
		Where("EXISTS (SELECT 1 FROM vnote_import_gates g WHERE g.id = ? AND g.knowledge_id = ? AND g.content_hash = ? AND g.attempt = ?)", "index", knowledgeID, contentHash, attempt).
		Where("NOT (" + r.vnoteControlPredicate() + ")").
		Updates(map[string]any{"parse_status": types.ParseStatusCancelled, "updated_at": time.Now().UTC()})
	return false, retired.Error
}

func (r *knowledgeRepository) CheckVNoteProcessing(ctx context.Context, knowledgeID, contentHash, attempt string) error {
	return r.checkVNoteAttemptStatus(ctx, knowledgeID, contentHash, attempt, types.ParseStatusProcessing)
}

func (r *knowledgeRepository) CheckVNotePending(ctx context.Context, knowledgeID, contentHash, attempt string) error {
	return r.checkVNoteAttemptStatus(ctx, knowledgeID, contentHash, attempt, types.ParseStatusPending)
}

func (r *knowledgeRepository) checkVNoteAttemptStatus(ctx context.Context, knowledgeID, contentHash, attempt, status string) error {
	extraction := "metadata->>'vnote_attempt'"
	if r.db.Dialector.Name() == "sqlite" {
		extraction = "json_extract(metadata, '$.vnote_attempt')"
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("id = ? AND parse_status = ?", knowledgeID, status).
		Where(extraction+" = ?", attempt).
		Where(r.vnoteControlPredicate()).
		Where("EXISTS (SELECT 1 FROM vnote_import_gates g WHERE g.id = ? AND g.knowledge_id = ? AND g.content_hash = ? AND g.attempt = ?)", "index", knowledgeID, contentHash, attempt).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("V-Note processing authorization was revoked")
	}
	return nil
}

// RetireRevokedVNotePending releases only a queued attempt. The conditional
// update races safely with worker claim: a processing attempt is never retired.
func RetireRevokedVNotePending(ctx context.Context, db *gorm.DB) error {
	r := &knowledgeRepository{db: db}
	result := db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("id IN (SELECT knowledge_id FROM vnote_import_gates WHERE id = ?)", "index").
		Where("parse_status = ? AND channel = ?", types.ParseStatusPending, "vnote").
		Where("NOT (" + r.vnoteControlPredicate() + ")").
		Updates(map[string]any{"parse_status": types.ParseStatusCancelled, "updated_at": time.Now().UTC()})
	return result.Error
}

// This predicate runs inside the mutation statement, so a committed control
// update cannot be followed by a successful stale claim or knowledge write.
func (r *knowledgeRepository) vnoteControlPredicate() string {
	workspace := "knowledges.metadata->>'vnote_workspace_id'"
	subject := "knowledges.metadata->>'vnote_subject'"
	version := "knowledges.metadata->>'vnote_config_version'"
	roots := "jsonb_exists(c.roots_json::jsonb, k.v_note_root_id)"
	if r.db.Dialector.Name() == "sqlite" {
		workspace = "json_extract(knowledges.metadata, '$.vnote_workspace_id')"
		subject = "json_extract(knowledges.metadata, '$.vnote_subject')"
		version = "json_extract(knowledges.metadata, '$.vnote_config_version')"
		roots = "EXISTS (SELECT 1 FROM json_each(c.roots_json) root WHERE root.value = k.v_note_root_id)"
	}
	return "EXISTS (SELECT 1 FROM knowledge_bases k JOIN vnote_controls c ON c.user_id = k.private_owner_id WHERE k.id = knowledges.knowledge_base_id AND c.workspace_id = " + workspace + " AND c.subject = " + subject + " AND CAST(c.version AS TEXT) = " + version + " AND c.enabled = TRUE AND " + roots + ")"
}
