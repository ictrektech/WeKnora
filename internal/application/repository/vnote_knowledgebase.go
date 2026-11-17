package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockVNoteKBProvisioning(ctx context.Context, tx *gorm.DB, scope types.VNoteRootScope, kb *types.KnowledgeBase) error {
	if scope.UserID == "" || scope.TenantID == 0 || kb.PrivateOwnerID != scope.UserID || kb.TenantID != scope.TenantID || kb.VNoteRootID != scope.RootID ||
		kb.ID != uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-kb\x00"+scope.UserID+"\x00"+scope.RootID)).String() {
		return ErrVNoteControlConflict
	}
	var control VNoteControl
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=? AND workspace_id=?", scope.UserID, scope.WorkspaceID).Take(&control).Error; err != nil {
		return err
	}
	allowed, err := VNoteControlAllows(ctx, tx, scope.UserID, scope.WorkspaceID, scope.Subject, scope.RootID, scope.ConfigVersion)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrVNoteControlStale
	}
	return nil
}

// RenameVNoteKnowledgeBase changes only the source-controlled name. It cannot
// resurrect a deleted KB or overwrite owner, model, storage or indexing config.
func RenameVNoteKnowledgeBase(ctx context.Context, db *gorm.DB, scope types.VNoteRootScope, kb *types.KnowledgeBase, name string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockVNoteKBProvisioning(ctx, tx, scope, kb); err != nil {
			return err
		}
		result := tx.Model(&types.KnowledgeBase{}).Where("id=? AND tenant_id=? AND private_owner_id=? AND v_note_root_id=?", kb.ID, scope.TenantID, scope.UserID, scope.RootID).
			Update("name", types.VNoteKnowledgeBaseName(name))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrKnowledgeBaseNotFound
		}
		return nil
	})
}
