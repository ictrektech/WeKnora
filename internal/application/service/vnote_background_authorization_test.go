package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type vnoteAuthorizationRepo struct {
	interfaces.KnowledgeRepository
	binding       repository.VNoteDelegation
	cancellations int
}

func (r *vnoteAuthorizationRepo) VNoteDelegationForKnowledge(context.Context, *types.Knowledge) (*repository.VNoteDelegation, error) {
	return &r.binding, nil
}
func (r *vnoteAuthorizationRepo) CancelVNoteAuthorizationAttempt(context.Context, *types.Knowledge) error {
	r.cancellations++
	return nil
}

func TestVNoteBackgroundAuthorizationChecksBothServicesWithoutPositiveCache(t *testing.T) {
	_, db, _, ctx, _ := vnoteFixture(t)
	require.NoError(t, db.AutoMigrate(&repository.VNoteDelegation{}))
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	secret := strings.Repeat("a", 64)
	binding := repository.VNoteDelegation{UserID: "owner", WorkspaceID: "space", TenantID: 7, Subject: "subject"}
	require.NoError(t, repository.StoreVNoteDelegation(ctx, db, binding, secret))
	require.NoError(t, db.First(&binding).Error)
	repo := &vnoteAuthorizationRepo{binding: binding}
	svc := &knowledgeService{repo: repo}
	note := &types.Knowledge{TenantID: 7, Metadata: []byte(`{"vnote_authorization":"delegated","vnote_owner_id":"owner","vnote_workspace_id":"space","vnote_subject":"subject","vnote_root_id":"root","vnote_note_id":"note","vnote_config_version":"3"}`)}
	active := true
	subject := "subject"
	platformCalls, sourceCalls := 0, 0
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platformCalls++
		require.Empty(t, r.Header.Get("Authorization"))
		require.Equal(t, "/v1000/oauth2/integration-grant/status", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "active": active, "subject": subject})
	}))
	defer platform.Close()
	allowed := true
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls++
		require.Equal(t, "/api/integrations/hybrag/authorize", r.URL.Path)
		require.Equal(t, "Integration "+secret, r.Header.Get("Authorization"))
		var scope map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&scope))
		require.Equal(t, "root", scope["rootId"])
		require.Equal(t, "note", scope["noteId"])
		require.Len(t, scope, 5, "checks carry only scope and source version, never note bodies or caller-supplied identities")
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "authorized": allowed, "subject": "subject", "workspaceId": "space", "configVersion": 3, "rootId": "root", "noteId": "note"})
	}))
	defer source.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL+"/v1000/oauth2/userinfo")
	t.Setenv("HYBRAG_VNOTE_URL", source.URL)
	require.NoError(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	active = false
	require.Error(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 2, platformCalls)
	require.Equal(t, 1, sourceCalls, "inactive platform status cannot advance to source authorization")
	require.Equal(t, 1, repo.cancellations)
	active = true
	allowed = false
	require.Error(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	subject = "someone-else"
	require.Error(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 2, sourceCalls, "another subject cannot query this source scope")
	require.Equal(t, 3, repo.cancellations)
}

func TestVNoteAuthorizationCancellationPreservesPublishedCopyAndReleasesIdleSlot(t *testing.T) {
	svc, db, _, ctx, snapshot := vnoteFixture(t)
	first, err := svc.Import(ctx, snapshot)
	require.NoError(t, err)
	finishVNoteGeneration(t, db, ctx, first.KnowledgeID)
	snapshot.Markdown += "\nNew source content"
	stage, err := svc.Import(ctx, snapshot)
	require.NoError(t, err)
	var note types.Knowledge
	require.NoError(t, db.First(&note, "id = ?", stage.KnowledgeID).Error)
	cancel := svc.knowledge.GetRepository().(interface {
		CancelVNoteAuthorizationAttempt(context.Context, *types.Knowledge) error
	})
	require.NoError(t, cancel.CancelVNoteAuthorizationAttempt(ctx, &note))
	var state types.VNoteImportState
	require.NoError(t, db.First(&state).Error)
	require.Equal(t, first.KnowledgeID, state.KnowledgeID)
	var published types.Knowledge
	require.NoError(t, db.First(&published, "id = ?", first.KnowledgeID).Error)
	require.Equal(t, types.ParseStatusCompleted, published.ParseStatus)
	snapshot.NoteID = "next-note"
	_, err = svc.Import(ctx, snapshot)
	require.NoError(t, err, "a boundary with no outstanding model call can release the processing slot")
}

func TestVNoteScheduledWorkerChecksWindowAtAdmissionButAllowsStartedDocumentToFinish(t *testing.T) {
	_, db, _, ctx, _ := vnoteFixture(t)
	require.NoError(t, db.AutoMigrate(&repository.VNoteDelegation{}))
	t.Setenv("HYBRAG_VNOTE_ENCRYPTION_KEY", strings.Repeat("k", 32))
	secret := strings.Repeat("a", 64)
	binding := repository.VNoteDelegation{UserID: "owner", WorkspaceID: "space", TenantID: 7, Subject: "subject"}
	require.NoError(t, repository.StoreVNoteDelegation(ctx, db, binding, secret))
	require.NoError(t, db.First(&binding).Error)
	repo := &vnoteAuthorizationRepo{binding: binding}
	svc := &knowledgeService{repo: repo}
	metadata := map[string]string{"vnote_authorization": "delegated", "vnote_scheduled": "true", "vnote_owner_id": "owner", "vnote_workspace_id": "space", "vnote_subject": "subject", "vnote_root_id": "root", "vnote_note_id": "note", "vnote_config_version": "3", "vnote_source_hash": strings.Repeat("b", 64), "vnote_source_title": "title"}
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	note := &types.Knowledge{TenantID: 7, ParseStatus: types.ParseStatusPending, Metadata: raw}
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "active": true, "subject": "subject"})
	}))
	defer platform.Close()
	hour := time.Now().UTC().Hour()
	schedule := vnoteSchedule{Mode: "night", IntervalMinutes: 15, TimeZone: "UTC", NightStart: (hour + 2) % 24, NightEnd: (hour + 3) % 24}
	planCalls, authorizeCalls := 0, 0
	paused, rootMissing := false, false
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.True(t, r.Header.Get("Authorization") == "Integration "+secret)
		if strings.HasSuffix(r.URL.Path, "/plan") {
			planCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "subject": "subject", "workspaceId": "space", "rootId": "root", "configVersion": 3, "schedule": schedule, "paused": paused, "rootMissing": rootMissing})
			return
		}
		authorizeCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "authorized": true, "subject": "subject", "workspaceId": "space", "rootId": "root", "noteId": "note", "configVersion": 3})
	}))
	defer source.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL+"/v1000/oauth2/userinfo")
	t.Setenv("HYBRAG_VNOTE_URL", source.URL)
	require.Error(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 1, repo.cancellations)
	require.Zero(t, authorizeCalls)
	schedule.Mode = "interval"
	require.NoError(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 2, planCalls)
	require.Equal(t, 1, authorizeCalls)
	schedule.Mode = "night"
	note.ParseStatus = types.ParseStatusProcessing
	require.NoError(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 2, planCalls)
	require.Equal(t, 2, authorizeCalls)
	note.ParseStatus = types.ParseStatusPending
	paused = true
	require.Error(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 2, repo.cancellations)
	paused = false
	rootMissing = true
	schedule.Mode = "interval"
	require.Error(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 3, repo.cancellations)
	require.Equal(t, 2, authorizeCalls, "a missing-root plan blocks model admission even before the source note check")
	// Explicit manual operations have no server-only scheduled marker.
	paused = false
	rootMissing = false
	delete(metadata, "vnote_scheduled")
	note.Metadata, err = json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, svc.stopUnauthorizedVNoteBoundary(ctx, note))
	require.Equal(t, 4, planCalls)
	require.Equal(t, 3, authorizeCalls)
}

func TestVNoteStalePendingCancellationCannotRetireAnAlreadyClaimedWorker(t *testing.T) {
	svc, db, _, ctx, snapshot := vnoteFixture(t)
	result, err := svc.Import(ctx, snapshot)
	require.NoError(t, err)
	var observed types.Knowledge
	require.NoError(t, db.First(&observed, "id=?", result.KnowledgeID).Error)
	require.Equal(t, types.ParseStatusPending, observed.ParseStatus)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", observed.ID).Update("parse_status", types.ParseStatusProcessing).Error)
	cancel := svc.knowledge.GetRepository().(interface {
		CancelVNoteAuthorizationAttempt(context.Context, *types.Knowledge) error
	})
	require.ErrorIs(t, cancel.CancelVNoteAuthorizationAttempt(ctx, &observed), repository.ErrVNoteDelegation)
	var current types.Knowledge
	require.NoError(t, db.First(&current, "id=?", observed.ID).Error)
	require.Equal(t, types.ParseStatusProcessing, current.ParseStatus)
	var generation types.VNoteImportGeneration
	require.NoError(t, db.First(&generation, "attempt=?", result.Attempt).Error)
	require.Equal(t, "staged", generation.Status)
	snapshot.NoteID = "next-note"
	_, err = svc.Import(ctx, snapshot)
	require.ErrorIs(t, err, ErrVNoteImportBusy)
}
