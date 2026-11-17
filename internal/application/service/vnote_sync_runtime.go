package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Bootstrap only missing/stale roots from already registered credentials. It
// never creates consent or reads source bodies. The bounded query converges
// across restarts and upgrades without rescanning every owner's notebook tree.
func (s *VNoteSyncService) bootstrap(ctx context.Context) error {
	selected := "selected.value"
	expand := "CROSS JOIN json_each(c.roots_json) selected"
	if s.db.Dialector.Name() != "sqlite" {
		selected = "selected.root_id"
		expand = "CROSS JOIN jsonb_array_elements_text(c.roots_json::jsonb) AS selected(root_id)"
	}
	var rows []struct {
		UserID, WorkspaceID, Subject, RootID string
		TenantID                             uint64
		ConfigVersion                        int64
	}
	err := s.db.WithContext(ctx).Raw(`SELECT d.user_id,d.workspace_id,d.subject,d.tenant_id,`+selected+` AS root_id,c.version AS config_version
  FROM vnote_controls c JOIN vnote_delegations d ON d.user_id=c.user_id AND d.workspace_id=c.workspace_id AND d.subject=c.subject
  `+expand+` LEFT JOIN vnote_sync_roots r ON r.user_id=d.user_id AND r.workspace_id=d.workspace_id AND r.root_id=`+selected+`
  WHERE c.enabled=? AND (r.id IS NULL OR r.config_version<c.version) ORDER BY d.user_id,d.workspace_id,`+selected+` LIMIT 16`, true).Scan(&rows).Error
	if err != nil {
		return err
	}
	for _, row := range rows {
		err = repository.EnsureVNoteSyncRoot(ctx, s.db, repository.VNoteDelegation{UserID: row.UserID, WorkspaceID: row.WorkspaceID, Subject: row.Subject, TenantID: row.TenantID}, row.RootID, row.ConfigVersion)
		if err != nil && !errors.Is(err, repository.ErrVNoteSyncStale) {
			return err
		}
	}
	return nil
}

// RunOnce intentionally excludes disabled/removed roots. Their published copies
// stay searchable; queue rows remain available if the owner enables them later.
func (s *VNoteSyncService) RunOnce(ctx context.Context) error {
	enabled := strings.TrimSpace(os.Getenv("HYBRAG_VOS_SSO_ENABLED"))
	if enabled == "" {
		enabled = strings.TrimSpace(os.Getenv("WEKNORA_VOS_SSO_ENABLED"))
	}
	if enabled != "" && !strings.EqualFold(enabled, "true") && enabled != "1" && !strings.EqualFold(enabled, "yes") {
		return nil
	}
	if err := s.bootstrap(ctx); err != nil {
		return err
	}
	selected := "selected.value"
	expand := "CROSS JOIN json_each(c.roots_json) selected"
	if s.db.Dialector.Name() != "sqlite" {
		selected = "selected.root_id"
		expand = "CROSS JOIN jsonb_array_elements_text(c.roots_json::jsonb) AS selected(root_id)"
	}
	var roots []types.VNoteSyncRoot
	err := s.db.WithContext(ctx).Raw(`SELECT r.* FROM vnote_sync_roots r
  JOIN vnote_controls c ON c.user_id=r.user_id AND c.workspace_id=r.workspace_id AND c.subject=r.subject AND c.version=r.config_version AND c.enabled=?
  JOIN vnote_delegations d ON d.user_id=r.user_id AND d.workspace_id=r.workspace_id AND d.subject=r.subject AND d.tenant_id=r.tenant_id
  `+expand+` WHERE `+selected+`=r.root_id AND r.next_poll_at<=? AND r.lease_until<=? ORDER BY r.next_poll_at,r.id LIMIT 4`, true, time.Now().UTC(), time.Now().UTC()).Scan(&roots).Error
	if err != nil {
		return err
	}
	for _, root := range roots {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = s.Step(ctx, root.ID)
	}
	return nil
}

// A single loop per process; database leases coordinate multiple replicas.
// The returned stop function cancels network calls and waits within its deadline.
func (s *VNoteSyncService) Start(parent context.Context) func(time.Duration) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
				logger.Warnf(ctx, "[vnote-sync] metadata maintenance deferred; queued state is retained")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func(timeout time.Duration) {
		cancel()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
	}
}
