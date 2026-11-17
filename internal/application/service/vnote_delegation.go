package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type VNoteDelegationBinding struct{ UserID, WorkspaceID, Subject string }

// This lookup can authenticate only a deny-only control operation when the
// platform grant is no longer active. It grants no document or status reads.
func (s *VNoteImportService) VNoteDelegationBinding(ctx context.Context, secret string) (*VNoteDelegationBinding, error) {
	if len(secret) != 64 {
		return nil, repository.ErrVNoteDelegation
	}
	var row repository.VNoteDelegation
	if err := s.database.WithContext(ctx).Where("secret_hash = ?", repository.VNoteSecretHash(secret)).Take(&row).Error; err != nil {
		return nil, repository.ErrVNoteDelegation
	}
	return &VNoteDelegationBinding{UserID: row.UserID, WorkspaceID: row.WorkspaceID, Subject: row.Subject}, nil
}

func (s *VNoteImportService) ApplyDelegatedControl(ctx context.Context, subject, workspaceID string, version int64, enabled bool, roots []string, secret string, active bool) error {
	caller := types.CallerFromContext(ctx)
	if caller.UserID == "" || caller.TenantID == 0 || caller.Role != types.TenantRoleOwner {
		return access.ErrForbidden
	}
	if !active {
		binding, err := s.VNoteDelegationBinding(ctx, secret)
		if err != nil || enabled || binding.UserID != caller.UserID || binding.WorkspaceID != workspaceID || binding.Subject != subject {
			return access.ErrForbidden
		}
	}
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repository.ApplyVNoteControl(ctx, tx, caller.UserID, workspaceID, subject, version, enabled, roots); err != nil {
			return err
		}
		if active {
			binding := repository.VNoteDelegation{UserID: caller.UserID, WorkspaceID: workspaceID, TenantID: caller.TenantID, Subject: subject}
			if err := repository.StoreVNoteDelegation(ctx, tx, binding, secret); err != nil {
				return err
			}
			if enabled {
				for _, root := range roots {
					if err := repository.EnsureVNoteSyncRoot(ctx, tx, binding, root, version); err != nil {
						return err
					}
				}
			}
		}
		return repository.RetireRevokedVNotePending(ctx, tx)
	})
}
