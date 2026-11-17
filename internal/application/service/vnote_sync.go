package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/platformidentity"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// VNoteSyncService reads metadata into a durable desired-state queue. Expensive
// work still uses the existing single-document gate and COW publishing path.
type VNoteSyncService struct {
	imports *VNoteImportService
	db      *gorm.DB
}

func NewVNoteSyncService(imports *VNoteImportService) *VNoteSyncService {
	return &VNoteSyncService{imports: imports, db: imports.database}
}

type vnoteSourceHTTPError struct{ status int }

func (e vnoteSourceHTTPError) Error() string { return "V-Note source is unavailable or changed" }

func vnoteUserinfoEndpoint() string {
	value := strings.TrimSpace(os.Getenv("HYBRAG_VOS_OIDC_USERINFO_URL"))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("WEKNORA_VOS_OIDC_USERINFO_URL"))
	}
	if value == "" {
		value = "http://172.17.0.1:8105/v1000/oauth2/userinfo"
	}
	return value
}

func vnoteSourceRequest(ctx context.Context, secret, path string, body, result any) error {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("HYBRAG_VNOTE_URL")), "/")
	if base == "" {
		base = "http://v-note-backend:8787"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return repository.ErrVNoteDelegation
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/integrations/hybrag/"+path, bytes.NewReader(payload))
	if err != nil {
		return repository.ErrVNoteDelegation
	}
	req.Header.Set("Authorization", "Integration "+secret)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return repository.ErrVNoteDelegation
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return vnoteSourceHTTPError{response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1048577))
	if err != nil || len(raw) > 1048576 {
		return repository.ErrVNoteDelegation
	}
	if json.Unmarshal(raw, result) != nil {
		return repository.ErrVNoteDelegation
	}
	return nil
}

func (s *VNoteSyncService) ownerContext(ctx context.Context, root types.VNoteSyncRoot) (context.Context, string, error) {
	var binding repository.VNoteDelegation
	if err := s.db.WithContext(ctx).Where("user_id=? AND workspace_id=? AND subject=? AND tenant_id=?", root.UserID, root.WorkspaceID, root.Subject, root.TenantID).Take(&binding).Error; err != nil {
		return ctx, "", repository.ErrVNoteDelegation
	}
	secret, err := repository.ReadVNoteDelegationSecret(binding)
	if err != nil {
		return ctx, "", err
	}
	identity, err := platformidentity.VerifyVNoteGrant(ctx, vnoteUserinfoEndpoint(), secret)
	if err != nil || identity.Subject != root.Subject {
		return ctx, "", repository.ErrVNoteDelegation
	}
	var user types.User
	if err := s.db.WithContext(ctx).Joins("JOIN platform_user_identities pi ON pi.user_id=users.id AND pi.platform_id=? AND pi.subject=?", vnoteUserinfoEndpoint(), root.Subject).
		Where("users.id=? AND users.tenant_id=? AND users.is_active=?", root.UserID, root.TenantID, true).Take(&user).Error; err != nil {
		return ctx, "", repository.ErrVNoteDelegation
	}
	var member types.TenantMember
	if err := s.db.WithContext(ctx).Where("user_id=? AND tenant_id=? AND role=? AND status=?", root.UserID, root.TenantID, types.TenantRoleOwner, types.TenantMemberStatusActive).Take(&member).Error; err != nil {
		return ctx, "", repository.ErrVNoteDelegation
	}
	var tenant types.Tenant
	if err := s.db.WithContext(ctx).First(&tenant, "id=?", root.TenantID).Error; err != nil {
		return ctx, "", repository.ErrVNoteDelegation
	}
	ctx = types.WithCaller(ctx, types.Caller{UserID: root.UserID, TenantID: root.TenantID, Role: types.TenantRoleOwner})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: root.UserID})
	ctx = types.WithExecutionTenant(ctx, root.TenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, root.UserID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &tenant)
	return types.WithVNoteDelegatedCredential(ctx, secret), secret, nil
}

// Step performs one bounded metadata page and at most one document operation.
// It is deliberately independent of timer/browser entrypoints for restart tests.
func (s *VNoteSyncService) Step(ctx context.Context, id string) error {
	claim, err := repository.ClaimVNoteSyncRoot(ctx, s.db, id, time.Now().UTC())
	if err != nil {
		return err
	}
	root := *claim
	workStatuses := []string{"pending", "indexing"}
	allowDeletion := true
	var modelWake time.Time
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		now := time.Now().UTC()
		next := root.NextMetadataAt
		if !modelWake.IsZero() && modelWake.Before(next) {
			next = modelWake
		}
		if next.Before(now) {
			next = now
		}
		// Exact status probes each use the due index with LIMIT 1. An IN/OR
		// followed by ordering would sort the whole pending backlog in SQLite.
		probe := func(status string, deletionOnly bool) {
			var pending types.VNoteSyncNote
			work := s.db.WithContext(cleanup).Select("next_attempt_at").Where("sync_root_id=? AND status=?", root.ID, status)
			if deletionOnly {
				work = work.Where("operation='delete'")
			}
			if work.Order("next_attempt_at,id").Take(&pending).Error == nil && pending.NextAttemptAt.Before(next) {
				next = pending.NextAttemptAt
				if next.Before(now) {
					next = now
				}
			}
		}
		for _, status := range workStatuses {
			probe(status, false)
		}
		if len(workStatuses) == 1 && allowDeletion {
			probe("pending", true)
		}
		if err != nil {
			root.Failures++
			delay := time.Duration(1<<min(root.Failures, 6)) * 15 * time.Second
			if delay > 15*time.Minute {
				delay = 15 * time.Minute
			}
			next = now.Add(delay)
		} else {
			root.Failures = 0
			root.ErrorCode = ""
		}
		_ = s.db.WithContext(cleanup).Model(&types.VNoteSyncRoot{}).Where("id=? AND lease_token=? AND config_version=?", root.ID, root.LeaseToken, root.ConfigVersion).
			Updates(map[string]any{"lease_until": now, "lease_token": "", "next_poll_at": next, "next_metadata_at": root.NextMetadataAt, "failures": root.Failures, "error_code": root.ErrorCode}).Error
	}()
	var secret string
	ctx, secret, err = s.ownerContext(ctx, root)
	if err != nil {
		root.ErrorCode = "authorization_unavailable"
		return err
	}
	var plan vnoteSyncPlan
	err = vnoteSourceRequest(ctx, secret, "plan", map[string]any{"rootId": root.RootID, "configVersion": root.ConfigVersion}, &plan)
	if err != nil || plan.Protocol != 1 || plan.Subject != root.Subject || plan.WorkspaceID != root.WorkspaceID || plan.RootID != root.RootID || plan.ConfigVersion != root.ConfigVersion || plan.Schedule == nil {
		root.ErrorCode = "plan_unavailable"
		if err == nil {
			err = repository.ErrVNoteSyncStale
		}
		return err
	}
	allowNew, nextWindow, planErr := plan.Schedule.window(time.Now().UTC())
	if plan.Paused || plan.RootMissing {
		allowNew = false
		nextWindow = time.Now().UTC().Add(15 * time.Minute)
	}
	if planErr != nil {
		err = planErr
		root.ErrorCode = "plan_invalid"
		return err
	}
	if plan.RootName != nil && !plan.RootMissing {
		_, err = s.imports.ensureVNoteKnowledgeBase(ctx, vnoteRootScope(ctx, root.WorkspaceID, root.Subject, root.RootID, root.ConfigVersion), *plan.RootName)
		if err != nil {
			root.ErrorCode = "knowledge_base_unavailable"
			return err
		}
	}
	if !allowNew {
		modelWake = nextWindow
		workStatuses = []string{"indexing"}
	}
	allowDeletion = !plan.Paused
	if plan.Paused {
		root.NextMetadataAt = nextWindow
	}
	// Metadata and deletions do not consume models and must not wait for a
	// night window or an explicit manual indexing run. Paused roots freeze both.
	if !plan.Paused && (root.Phase == "snapshot" || !root.NextMetadataAt.After(time.Now().UTC())) {
		body := map[string]any{"rootId": root.RootID, "configVersion": root.ConfigVersion}
		if root.Phase == "snapshot" {
			if root.SyncIdentity != "" {
				body["syncIdentity"], body["graphRevision"], body["startCursor"], body["startCursorStamp"] = root.SyncIdentity, root.GraphRevision, root.Cursor, root.CursorStamp
			}
			body["afterNotebookId"], body["afterNoteId"] = root.AfterNotebookID, root.AfterNoteID
		} else {
			body["syncIdentity"], body["graphRevision"], body["cursor"], body["cursorStamp"] = root.SyncIdentity, root.GraphRevision, root.Cursor, root.CursorStamp
		}
		var page types.VNoteSourcePage
		err = vnoteSourceRequest(ctx, secret, root.Phase, body, &page)
		var remote vnoteSourceHTTPError
		if errors.As(err, &remote) && remote.status == 409 || err == nil && page.Rescan {
			err = repository.ResetVNoteSyncSnapshot(ctx, s.db, root)
			root.NextMetadataAt = time.Now().UTC()
			return err
		}
		if err != nil {
			root.ErrorCode = "source_unavailable"
			return err
		}
		err = repository.StoreVNoteSourcePage(ctx, s.db, root, page)
		if err != nil {
			root.ErrorCode = "source_page_invalid"
			return err
		}
		err = s.db.WithContext(ctx).Where("id=? AND lease_token=?", root.ID, root.LeaseToken).Take(&root).Error
		if err != nil {
			return err
		}
		root.NextMetadataAt = time.Now().UTC().Add(time.Duration(plan.Schedule.IntervalMinutes) * time.Minute)
		if page.HasMore {
			root.NextMetadataAt = time.Now().UTC()
		}
	}
	if root.Phase == "snapshot" && !plan.Paused {
		return nil
	}
	var desired types.VNoteSyncNote
	err = s.db.WithContext(ctx).Where("sync_root_id=? AND status=? AND next_attempt_at<=?", root.ID, "indexing", time.Now().UTC()).Order("next_attempt_at,id").Take(&desired).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && (allowNew || allowDeletion) {
		pending := s.db.WithContext(ctx).Where("sync_root_id=? AND status=? AND next_attempt_at<=?", root.ID, "pending", time.Now().UTC())
		if !allowNew {
			pending = pending.Where("operation='delete'")
		}
		err = pending.Order("next_attempt_at,id").Take(&desired).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
		return nil
	}
	if err != nil {
		return err
	}
	err = s.processDesired(ctx, secret, &root, desired, *plan.Schedule)
	if err != nil {
		root.ErrorCode = "document_pending"
	}
	return err
}

func (s *VNoteSyncService) processDesired(ctx context.Context, secret string, root *types.VNoteSyncRoot, note types.VNoteSyncNote, schedule vnoteSchedule) error {
	now := time.Now().UTC()
	retry := now.Add(10 * time.Second)
	save := func(status, id, attempt, code string) error {
		return repository.UpdateVNoteSyncNote(ctx, s.db, *root, note, status, id, attempt, code, retry)
	}
	if note.Operation == "delete" {
		return repository.ApplyVNoteSourceDeletion(ctx, s.db, *root, note)
	}
	if note.Status == "indexing" {
		var status struct {
			ParseStatus, ErrorMessage, GenerationStatus, PublishedID string
			UpdatedAt                                                time.Time
		}
		query := s.db.WithContext(ctx).Table("vnote_import_generations gen").Select("k.parse_status,k.error_message,k.updated_at,gen.status AS generation_status,s.knowledge_id AS published_id").
			Joins("JOIN knowledges k ON k.id=gen.knowledge_id AND k.tenant_id=? AND k.deleted_at IS NULL", root.TenantID).
			Joins("JOIN vnote_import_states s ON s.id=gen.state_id AND s.user_id=gen.user_id").
			Where("gen.attempt=? AND gen.knowledge_id=? AND gen.state_id=? AND gen.user_id=? AND gen.workspace_id=? AND gen.subject=? AND gen.root_id=?", note.Attempt, note.KnowledgeID, note.ID, root.UserID, root.WorkspaceID, root.Subject, root.RootID).Take(&status)
		if query.Error != nil && !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return query.Error
		}
		if note.Attempt == "" || query.Error != nil {
			return save("blocked", note.KnowledgeID, note.Attempt, "attempt_unknown")
		}
		if status.ErrorMessage != "" && status.ParseStatus == types.ParseStatusProcessing {
			return save("blocked", note.KnowledgeID, note.Attempt, "recovery_required")
		}
		switch status.ParseStatus {
		case types.ParseStatusCompleted:
			if status.GenerationStatus != "published" || status.PublishedID != note.KnowledgeID {
				return save("blocked", note.KnowledgeID, note.Attempt, "attempt_unknown")
			}
			return save("done", note.KnowledgeID, note.Attempt, "")
		case types.ParseStatusPending:
			if time.Since(status.UpdatedAt) >= 90*time.Second {
				if repair, ok := s.imports.knowledge.(interface {
					RecoverVNotePending(context.Context, string, string) error
				}); ok {
					if err := repair.RecoverVNotePending(ctx, note.KnowledgeID, note.Attempt); err != nil {
						return err
					}
				}
				retry = now.Add(time.Minute)
			}
			return save("indexing", note.KnowledgeID, note.Attempt, "")
		case types.ParseStatusProcessing, types.ParseStatusFinalizing:
			return save("indexing", note.KnowledgeID, note.Attempt, "")
		case types.ParseStatusCancelled:
			return save("pending", "", "", "")
		default:
			return save("blocked", note.KnowledgeID, note.Attempt, "recovery_required")
		}
	}
	if !vnoteSourceVersion.MatchString(note.SourceHash) || len(note.Title) > 2000 {
		return save("blocked", "", "", "unsupported_note")
	}
	// Waiting for the global model slot must not repeatedly export note bodies.
	var occupied struct{ KnowledgeID, Attempt, ParseStatus, ErrorMessage string }
	gate := s.db.WithContext(ctx).Table("vnote_import_gates g").Select("g.knowledge_id,g.attempt,k.parse_status,k.error_message").
		Joins("JOIN knowledges k ON k.id=g.knowledge_id AND k.tenant_id=g.tenant_id").Where("g.id=? AND k.parse_status IN ?", "index", []string{types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing}).Take(&occupied)
	if gate.Error != nil && !errors.Is(gate.Error, gorm.ErrRecordNotFound) {
		return gate.Error
	}
	if gate.Error == nil {
		if occupied.Attempt == note.Attempt && occupied.KnowledgeID == note.KnowledgeID && occupied.ErrorMessage != "" {
			return save("blocked", note.KnowledgeID, note.Attempt, "recovery_required")
		}
		return save("pending", note.KnowledgeID, note.Attempt, "")
	}
	var snapshot types.VNoteSnapshot
	err := vnoteSourceRequest(ctx, secret, "export-note", map[string]any{"rootId": root.RootID, "configVersion": root.ConfigVersion, "noteId": note.NoteID}, &snapshot)
	var remote vnoteSourceHTTPError
	if errors.As(err, &remote) {
		if remote.status == 422 {
			return save("blocked", "", "", "unsupported_note")
		}
		if remote.status == 404 || remote.status == 409 {
			root.NextMetadataAt = now
			return save("pending", "", "", "source_changed")
		}
	}
	if err != nil {
		return err
	}
	if snapshot.Subject != root.Subject || snapshot.WorkspaceID != root.WorkspaceID || snapshot.ConfigVersion != root.ConfigVersion || snapshot.RootID != root.RootID || snapshot.NoteID != note.NoteID {
		return repository.ErrVNoteSyncStale
	}
	if snapshot.SourceHash != note.SourceHash || snapshot.Title != note.Title {
		root.NextMetadataAt = now
		return save("pending", "", "", "source_changed")
	}
	allowed, next, err := schedule.window(time.Now().UTC())
	if err != nil {
		return err
	}
	if !allowed {
		root.NextMetadataAt = next
		return save("pending", "", "", "")
	}
	result, err := s.imports.Import(types.WithVNoteScheduledOperation(ctx), snapshot)
	if errors.Is(err, ErrVNoteImportBusy) {
		return save("pending", note.KnowledgeID, note.Attempt, "")
	}
	if err != nil {
		return err
	}
	if result == nil || !result.Accepted {
		return repository.ErrVNoteSyncStale
	}
	if result.State == types.ParseStatusCompleted {
		return save("done", result.KnowledgeID, result.Attempt, "")
	}
	if result.Attempt == "" {
		return save("blocked", result.KnowledgeID, "", "attempt_unknown")
	}
	return save("indexing", result.KnowledgeID, result.Attempt, "")
}
