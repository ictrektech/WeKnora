package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrVNoteGenerationSuperseded = errors.New("V-Note generation is no longer publishable")

// ResolveVNoteKnowledgeID maps a stable source ID or an older published
// physical ID to the current physical document. Unpublished IDs never alias.
func (r *knowledgeRepository) ResolveVNoteKnowledgeID(ctx context.Context, id string) (string, error) {
	if id == "" || !r.db.Migrator().HasTable(&types.VNoteImportState{}) {
		return id, nil
	}
	var physical string
	err := r.db.WithContext(ctx).Raw(`SELECT s.knowledge_id FROM vnote_import_states s
		WHERE s.id = ? OR EXISTS (SELECT 1 FROM vnote_import_generations g
		WHERE g.state_id = s.id AND g.user_id = s.user_id AND g.knowledge_id = ? AND g.status = 'published')
		LIMIT 1`, id, id).Row().Scan(&physical)
	if errors.Is(err, sql.ErrNoRows) {
		return id, nil
	}
	if err != nil {
		return "", err
	}
	if physical == "" {
		return "", ErrKnowledgeNotFound
	}
	return physical, nil
}

// PublicVNoteKnowledgeIDs translates current physical IDs for response bodies.
func (r *knowledgeRepository) PublicVNoteKnowledgeIDs(ctx context.Context, ids []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(ids) == 0 || !r.db.Migrator().HasTable(&types.VNoteImportState{}) {
		return result, nil
	}
	var rows []struct {
		ID          string
		KnowledgeID string
	}
	err := r.db.WithContext(ctx).Table("vnote_import_states AS s").Select("s.id, s.knowledge_id").
		Joins("JOIN knowledge_bases kb ON kb.id = s.knowledge_base_id AND kb.private_owner_id = s.user_id AND kb.v_note_root_id <> '' AND kb.deleted_at IS NULL").
		Where("s.knowledge_id IN ? AND s.knowledge_id <> ''", ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.KnowledgeID] = row.ID
	}
	return result, nil
}

func (r *knowledgeRepository) resolveVNoteLogicalIDs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 || !r.db.Migrator().HasTable(&types.VNoteImportState{}) {
		return ids, nil
	}
	var states []types.VNoteImportState
	if err := r.db.WithContext(ctx).Where("id IN ? AND knowledge_id <> ''", ids).Find(&states).Error; err != nil {
		return nil, err
	}
	mapping := make(map[string]string, len(states))
	for _, state := range states {
		mapping[state.ID] = state.KnowledgeID
	}
	var previous []struct {
		PreviousID string
		CurrentID  string
	}
	if err := r.db.WithContext(ctx).Table("vnote_import_generations AS g").
		Select("g.knowledge_id AS previous_id, s.knowledge_id AS current_id").
		Joins("JOIN vnote_import_states s ON s.id = g.state_id AND s.user_id = g.user_id").
		Where("g.knowledge_id IN ? AND g.status = ? AND s.knowledge_id <> ''", ids, "published").Scan(&previous).Error; err != nil {
		return nil, err
	}
	for _, row := range previous {
		mapping[row.PreviousID] = row.CurrentID
	}
	resolved := append([]string(nil), ids...)
	for i, id := range resolved {
		if value := mapping[id]; value != "" {
			resolved[i] = value
		}
	}
	return resolved, nil
}

func (r *knowledgeRepository) createVNoteGeneration(ctx context.Context, knowledge *types.Knowledge) error {
	attempt := types.VNoteAdmissionFromContext(ctx)
	if attempt == "" {
		return ErrVNoteGenerationSuperseded
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation types.VNoteImportGeneration
		if err := tx.Where("attempt = ? AND knowledge_id = ? AND status = ?", attempt, knowledge.ID, "staged").First(&generation).Error; err != nil {
			return err
		}
		metadata := knowledge.GetMetadata()
		if metadata["vnote_attempt"] != attempt || metadata["vnote_workspace_id"] != generation.WorkspaceID || metadata["vnote_subject"] != generation.Subject || metadata["vnote_root_id"] != generation.RootID || metadata["vnote_config_version"] != strconv.FormatInt(generation.ConfigVersion, 10) || metadata["vnote_content_hash"] != generation.ContentHash {
			return ErrVNoteGenerationSuperseded
		}
		var control VNoteControl
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND workspace_id = ?", generation.UserID, generation.WorkspaceID).First(&control).Error; err != nil {
			return err
		}
		allowed, err := VNoteControlAllows(ctx, tx, generation.UserID, generation.WorkspaceID, generation.Subject, generation.RootID, generation.ConfigVersion)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrVNoteGenerationSuperseded
		}
		var count int64
		if err := tx.Model(&types.KnowledgeBase{}).Where("id = ? AND tenant_id = ? AND private_owner_id = ? AND v_note_root_id = ?", knowledge.KnowledgeBaseID, knowledge.TenantID, generation.UserID, generation.RootID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrVNoteGenerationSuperseded
		}
		if err := tx.Model(&types.VNoteImportState{}).Where("id = ? AND user_id = ? AND knowledge_base_id = ? AND knowledge_id = ?", generation.StateID, generation.UserID, knowledge.KnowledgeBaseID, generation.ExpectedKnowledgeID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrVNoteGenerationSuperseded
		}
		if err := tx.Model(&types.VNoteImportGate{}).Where("id = ? AND tenant_id = ? AND knowledge_id = ? AND attempt = ?", "index", knowledge.TenantID, knowledge.ID, attempt).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrVNoteGenerationSuperseded
		}
		return tx.Create(knowledge).Error
	})
}

func visibleVNoteGenerations(query *gorm.DB) *gorm.DB {
	return query.Where(`(NOT EXISTS (SELECT 1 FROM knowledge_bases vk WHERE vk.id = knowledges.knowledge_base_id AND vk.v_note_root_id <> '')
		OR EXISTS (SELECT 1 FROM vnote_import_states vs JOIN knowledge_bases vk ON vk.id = vs.knowledge_base_id
		WHERE vs.knowledge_id = knowledges.id AND vs.knowledge_base_id = knowledges.knowledge_base_id
		AND vs.user_id = vk.private_owner_id AND vk.v_note_root_id <> ''))`)
}

// HiddenVNoteGenerationIDs returns physical documents that are not the
// current published pointer. Existing source-state pointers cover legacy
// documents; missing knowledge, KB, or source state is never visible.
func (r *knowledgeRepository) HiddenVNoteGenerationIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	hidden := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			hidden[id] = true
		}
	}
	visible, err := r.VisibleKnowledgeScopes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id := range visible {
		delete(hidden, id)
	}
	return hidden, nil
}

// VisibleKnowledgeScopes positively verifies each hit against the live
// document and its actual KB, including V-Note's published pointer.
func (r *knowledgeRepository) VisibleKnowledgeScopes(ctx context.Context, ids []string) (map[string]string, error) {
	visible := make(map[string]string)
	if len(ids) == 0 {
		return visible, nil
	}
	var rows []struct {
		KnowledgeID     string
		KnowledgeBaseID string
	}
	err := r.db.WithContext(ctx).Table("knowledges AS k").Select("k.id AS knowledge_id, k.knowledge_base_id").
		Joins("JOIN knowledge_bases kb ON kb.id = k.knowledge_base_id AND kb.tenant_id = k.tenant_id").
		Where("k.id IN ? AND k.deleted_at IS NULL AND kb.deleted_at IS NULL", ids).
		Where(`(kb.v_note_root_id = '' OR EXISTS (SELECT 1 FROM vnote_import_states s
			WHERE s.knowledge_id = k.id AND s.knowledge_base_id = kb.id AND s.user_id = kb.private_owner_id))`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		visible[row.KnowledgeID] = row.KnowledgeBaseID
	}
	return visible, nil
}

// PublishVNoteGeneration makes a completely indexed physical document visible
// by switching the source pointer in the same transaction as the guarded final
// knowledge write. The control row lock orders publication against revocation.
func PublishVNoteGeneration(ctx context.Context, db *gorm.DB, attempt, expectedPublishedID string, knowledge *types.Knowledge) error {
	if attempt == "" || knowledge == nil {
		return ErrVNoteGenerationSuperseded
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation types.VNoteImportGeneration
		if err := tx.Where("attempt = ? AND status = ? AND knowledge_id = ?", attempt, "staged", knowledge.ID).First(&generation).Error; err != nil {
			return err
		}
		if generation.ExpectedKnowledgeID != expectedPublishedID {
			return ErrVNoteGenerationSuperseded
		}
		var control VNoteControl
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND workspace_id = ?", generation.UserID, generation.WorkspaceID).First(&control).Error; err != nil {
			return err
		}
		allowed, err := VNoteControlAllows(ctx, tx, generation.UserID, generation.WorkspaceID, generation.Subject, generation.RootID, generation.ConfigVersion)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrVNoteGenerationSuperseded
		}
		var state types.VNoteImportState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND knowledge_id = ?", generation.StateID, generation.UserID, expectedPublishedID).First(&state).Error; err != nil {
			return ErrVNoteGenerationSuperseded
		}
		if knowledge.KnowledgeBaseID != state.KnowledgeBaseID || knowledge.ParseStatus != types.ParseStatusCompleted {
			return ErrVNoteGenerationSuperseded
		}
		if expectedPublishedID != "" {
			legacy := types.VNoteImportGeneration{Attempt: uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-legacy\x00"+expectedPublishedID)).String(),
				StateID: state.ID, KnowledgeID: expectedPublishedID, UserID: state.UserID, WorkspaceID: generation.WorkspaceID,
				Subject: generation.Subject, RootID: generation.RootID, ConfigVersion: generation.ConfigVersion, ContentHash: state.ContentHash, Status: "published"}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&legacy).Error; err != nil {
				return err
			}
		}
		if err := (&knowledgeRepository{db: tx}).updateVNoteKnowledge(ctx, knowledge, attempt); err != nil {
			return err
		}
		updated := tx.Model(&types.VNoteImportState{}).Where("id = ? AND user_id = ? AND knowledge_id = ?", state.ID, state.UserID, expectedPublishedID).
			Updates(map[string]any{"knowledge_id": generation.KnowledgeID, "content_hash": generation.ContentHash})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrVNoteGenerationSuperseded
		}
		updated = tx.Model(&types.VNoteImportGeneration{}).Where("attempt = ? AND status = ?", attempt, "staged").Update("status", "published")
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrVNoteGenerationSuperseded
		}
		return nil
	})
}

func (r *knowledgeRepository) PublishVNoteGeneration(ctx context.Context, attempt string, knowledge *types.Knowledge) error {
	var generation types.VNoteImportGeneration
	if err := r.db.WithContext(ctx).Where("attempt = ? AND knowledge_id = ? AND status = ?", attempt, knowledge.ID, "staged").First(&generation).Error; err != nil {
		return err
	}
	return PublishVNoteGeneration(ctx, r.db, attempt, generation.ExpectedKnowledgeID, knowledge)
}
