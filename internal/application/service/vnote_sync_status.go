package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

type VNoteSyncStatus struct {
	RootID        string    `json:"rootId"`
	ConfigVersion int64     `json:"configVersion"`
	State         string    `json:"state"`
	NextCheckAt   time.Time `json:"nextCheckAt"`
	ErrorCode     string    `json:"errorCode,omitempty"`
}

// Polling uses indexed existence checks, not recurring full-queue counts or
// document/body scans. Scope is authenticated independently by the handler.
func (s *VNoteImportService) SynchronizationStatus(ctx context.Context, subject, workspace string, roots []string) ([]VNoteSyncStatus, error) {
	caller := types.CallerFromContext(ctx)
	if caller.UserID == "" || caller.TenantID == 0 || !caller.Role.HasPermission(types.TenantRoleOwner) || workspace == "" || len(roots) > 100 {
		return nil, access.ErrForbidden
	}
	result := make([]VNoteSyncStatus, 0, len(roots))
	if len(roots) == 0 {
		return result, nil
	}
	var rows []struct {
		RootID            string
		ConfigVersion     int64
		Phase, ErrorCode  string
		NextMetadataAt    time.Time
		Indexing, Pending bool
		BlockedError      string
		RecoveryRequired  bool
	}
	err := s.database.WithContext(ctx).Table("vnote_sync_roots r").Select(`r.root_id,r.config_version,r.phase,r.error_code,r.next_metadata_at,
  EXISTS (SELECT 1 FROM vnote_sync_notes n WHERE n.sync_root_id=r.id AND n.status='indexing') AS indexing,
  EXISTS (SELECT 1 FROM vnote_sync_notes n WHERE n.sync_root_id=r.id AND n.status='pending') AS pending,
  COALESCE((SELECT n.error_code FROM vnote_sync_notes n WHERE n.sync_root_id=r.id AND n.status='blocked' ORDER BY n.next_attempt_at,n.id LIMIT 1),'') AS blocked_error,
  EXISTS (SELECT 1 FROM vnote_import_gates g JOIN knowledges k ON k.id=g.knowledge_id AND k.tenant_id=g.tenant_id
   JOIN knowledge_bases kb ON kb.id=k.knowledge_base_id AND kb.tenant_id=k.tenant_id
   WHERE g.id='index' AND kb.private_owner_id=r.user_id AND kb.v_note_root_id=r.root_id AND kb.tenant_id=r.tenant_id
   AND k.parse_status='processing' AND COALESCE(k.error_message,'')<>'') AS recovery_required`).
		Where("r.user_id=? AND r.tenant_id=? AND r.subject=? AND r.workspace_id=? AND r.root_id IN ?", caller.UserID, caller.TenantID, subject, workspace, roots).
		Order("r.root_id").Limit(100).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		state := "waiting"
		switch {
		case row.RecoveryRequired:
			state = "attention"
		case row.ErrorCode != "":
			state = "unavailable"
		case row.Indexing:
			state = "indexing"
		case row.Phase == "snapshot":
			state = "scanning"
		case row.Pending:
			state = "pending"
		case row.BlockedError != "":
			state = "attention"
		}
		code := row.ErrorCode
		if row.RecoveryRequired {
			code = "recovery_required"
		}
		if code == "" {
			code = row.BlockedError
		}
		result = append(result, VNoteSyncStatus{RootID: row.RootID, ConfigVersion: row.ConfigVersion, State: state, NextCheckAt: row.NextMetadataAt, ErrorCode: code})
	}
	return result, nil
}
