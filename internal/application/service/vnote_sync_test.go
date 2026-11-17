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
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type syncSourceFixture struct {
	snapshot                          types.VNoteSnapshot
	sequence                          int64
	exports, reads                    int
	active, deleted, outage, redirect bool
	schedule                          vnoteSchedule
	paused                            bool
	rootName                          string
	rootMissing                       bool
	graphRevision                     int64
}

func syncServiceFixture(t *testing.T) (*VNoteSyncService, *gorm.DB, *vnoteTestKnowledge, context.Context, *syncSourceFixture, string) {
	t.Helper()
	imports, db, knowledge, ctx, snapshot := vnoteFixture(t)
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.Tenant{}, &types.TenantMember{}, &types.PlatformIdentity{}, &repository.VNoteDelegation{}, &types.VNoteSyncRoot{}, &types.VNoteSyncNote{}))
	require.NoError(t, db.Create(&types.Tenant{ID: 7, Name: "Personal"}).Error)
	require.NoError(t, db.Create(&types.User{ID: "alice", Username: "alice", Email: "alice@example.test", TenantID: 7, IsActive: true}).Error)
	require.NoError(t, db.Create(&types.TenantMember{UserID: "alice", TenantID: 7, Role: types.TenantRoleOwner, Status: types.TenantMemberStatusActive}).Error)
	snapshot.SourceHash = strings.Repeat("b", 64)
	state := &syncSourceFixture{snapshot: snapshot, rootName: "Notebook", graphRevision: 1, sequence: 1, active: true, schedule: vnoteSchedule{Mode: "interval", IntervalMinutes: 15, TimeZone: "UTC", NightStart: 0, NightEnd: 6}}
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "active": state.active, "subject": "subject"})
	}))
	t.Cleanup(platform.Close)
	endpoint := platform.URL + "/v1000/oauth2/userinfo"
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", endpoint)
	require.NoError(t, db.Create(&types.PlatformIdentity{PlatformID: endpoint, Subject: "subject", UserID: "alice"}).Error)
	t.Setenv("HYBRAG_VNOTE_ENCRYPTION_KEY", strings.Repeat("k", 32))
	binding := repository.VNoteDelegation{UserID: "alice", WorkspaceID: "personal", Subject: "subject", TenantID: 7}
	secret := strings.Repeat("a", 64)
	require.NoError(t, repository.StoreVNoteDelegation(ctx, db, binding, secret))
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.True(t, r.Header.Get("Authorization") == "Integration "+secret)
		if state.outage {
			w.WriteHeader(503)
			return
		}
		if state.redirect {
			http.Redirect(w, r, "https://example.invalid", 302)
			return
		}
		var input struct {
			Cursor        int64 `json:"cursor"`
			GraphRevision int64 `json:"graphRevision"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		path := strings.TrimPrefix(r.URL.Path, "/api/integrations/hybrag/")
		if path == "plan" {
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "subject": "subject", "workspaceId": "personal", "rootId": "root", "configVersion": 1, "schedule": state.schedule, "paused": state.paused, "rootName": state.rootName, "rootMissing": state.rootMissing})
			return
		}
		if path == "export-note" {
			state.exports++
			if state.deleted || state.rootMissing {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(state.snapshot)
			return
		}
		if path == "authorize" {
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": 1, "authorized": !state.deleted && !state.paused && !state.rootMissing, "subject": "subject", "workspaceId": "personal", "rootId": "root", "noteId": "note", "configVersion": 1})
			return
		}
		state.reads++
		page := types.VNoteSourcePage{Protocol: 1, Subject: "subject", WorkspaceID: "personal", RootID: "root", ConfigVersion: 1, RootName: state.rootName, SyncIdentity: "source-1", GraphRevision: state.graphRevision, SnapshotCursor: state.sequence, SnapshotCursorStamp: strings.Repeat("c", 64), Cursor: state.sequence, CursorStamp: strings.Repeat("c", 64), Notes: []types.VNoteSourceNote{}, Events: []types.VNoteSourceNote{}}
		if state.rootMissing {
			page.RootName = ""
		}
		if path == "changes" {
			page.Rescan = input.GraphRevision != state.graphRevision
		}
		note := types.VNoteSourceNote{NoteID: "note", NotebookID: "child", Title: state.snapshot.Title, SourceHash: state.snapshot.SourceHash, UpdatedAt: state.snapshot.UpdatedAt, Operation: "upsert", Sequence: state.sequence}
		if path == "snapshot" && !state.deleted && !state.rootMissing {
			page.Notes = append(page.Notes, note)
			page.AfterNotebookID = "child"
			page.AfterNoteID = "note"
		}
		if path == "changes" && input.Cursor < state.sequence {
			if state.deleted || state.rootMissing {
				note = types.VNoteSourceNote{NoteID: "note", Operation: "delete", Sequence: state.sequence}
			}
			page.Events = append(page.Events, note)
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	t.Cleanup(source.Close)
	t.Setenv("HYBRAG_VNOTE_URL", source.URL)
	require.NoError(t, repository.EnsureVNoteSyncRoot(ctx, db, binding, "root", 1))
	return NewVNoteSyncService(imports), db, knowledge, ctx, state, repository.VNoteSyncRootID("alice", "personal", "root")
}
func wakeSync(t *testing.T, db *gorm.DB, id string, metadata bool) {
	t.Helper()
	now := time.Now().UTC().Add(-time.Second)
	updates := map[string]any{"next_poll_at": now}
	if metadata {
		updates["next_metadata_at"] = now
	}
	require.NoError(t, db.Model(&types.VNoteSyncRoot{}).Where("id=?", id).Updates(updates).Error)
	require.NoError(t, db.Model(&types.VNoteSyncNote{}).Where("sync_root_id=?", id).Update("next_attempt_at", now).Error)
}
func syncDesired(t *testing.T, db *gorm.DB, id string) types.VNoteSyncNote {
	t.Helper()
	var note types.VNoteSyncNote
	require.NoError(t, db.Where("sync_root_id=? AND note_id=?", id, "note").Take(&note).Error)
	return note
}

func TestVNoteSyncSurvivesRestartAndSkipsUnchangedBody(t *testing.T) {
	svc, db, knowledge, ctx, state, id := syncServiceFixture(t)
	require.NoError(t, svc.Step(context.Background(), id))
	note := syncDesired(t, db, id)
	require.Equal(t, "indexing", note.Status)
	require.NotEmpty(t, note.Attempt)
	require.Equal(t, 1, knowledge.published)
	require.Equal(t, 1, state.exports)
	require.Equal(t, "true", knowledge.last.SourceMetadata["vnote_scheduled"])
	finishVNoteGeneration(t, db, ctx, note.KnowledgeID)
	// A fresh service instance recovers only persisted queue state; it does not
	// refetch metadata/body while waiting for the exact attempt to complete.
	svc = NewVNoteSyncService(svc.imports)
	wakeSync(t, db, id, false)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Equal(t, "done", syncDesired(t, db, id).Status)
	require.Equal(t, 1, state.exports)
	require.Equal(t, 1, state.reads)
	state.sequence++
	wakeSync(t, db, id, true)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Equal(t, 1, state.exports)
	require.Equal(t, 1, knowledge.published)
	// A changed body creates a new physical version while retaining the pointer.
	state.sequence++
	state.snapshot.SourceHash = strings.Repeat("d", 64)
	state.snapshot.Markdown = "changed body"
	wakeSync(t, db, id, true)
	require.NoError(t, svc.Step(context.Background(), id))
	newNote := syncDesired(t, db, id)
	require.NotEqual(t, note.KnowledgeID, newNote.KnowledgeID)
	require.Equal(t, 2, state.exports)
	require.Equal(t, 2, knowledge.published)
	var published types.VNoteImportState
	require.NoError(t, db.First(&published, "id=?", note.ID).Error)
	require.Equal(t, note.KnowledgeID, published.KnowledgeID)
}

func TestVNoteSyncDeletionNeedsNoBodyOrModelAndOutageKeepsCopy(t *testing.T) {
	svc, db, knowledge, ctx, state, id := syncServiceFixture(t)
	require.NoError(t, svc.Step(context.Background(), id))
	note := syncDesired(t, db, id)
	finishVNoteGeneration(t, db, ctx, note.KnowledgeID)
	wakeSync(t, db, id, false)
	require.NoError(t, svc.Step(context.Background(), id))
	state.outage = true
	wakeSync(t, db, id, true)
	require.Error(t, svc.Step(context.Background(), id))
	var published types.VNoteImportState
	require.NoError(t, db.First(&published, "id=?", note.ID).Error)
	require.Equal(t, note.KnowledgeID, published.KnowledgeID)
	state.outage = false
	state.deleted = true
	state.sequence++
	wakeSync(t, db, id, true)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Equal(t, "done", syncDesired(t, db, id).Status)
	require.NoError(t, db.First(&published, "id=?", note.ID).Error)
	require.Empty(t, published.KnowledgeID)
	require.Equal(t, 1, state.exports)
	require.Equal(t, 1, knowledge.published)
}

func TestVNoteSyncRevokedPlatformAndRedirectNeverReadContent(t *testing.T) {
	for _, mode := range []string{"revoked", "redirect", "wrong_owner"} {
		t.Run(mode, func(t *testing.T) {
			svc, db, knowledge, _, state, id := syncServiceFixture(t)
			if mode == "revoked" {
				state.active = false
			}
			if mode == "redirect" {
				state.redirect = true
			}
			if mode == "wrong_owner" {
				require.NoError(t, db.Model(&types.PlatformIdentity{}).Where("subject=?", "subject").Update("user_id", "other").Error)
			}
			require.Error(t, svc.Step(context.Background(), id))
			require.Zero(t, knowledge.published)
			require.Zero(t, state.exports)
		})
	}
}

func TestVNoteSyncManualAndClosedNightWindowMaintainMetadataWithoutModels(t *testing.T) {
	for _, mode := range []string{"manual", "night"} {
		t.Run(mode, func(t *testing.T) {
			svc, db, knowledge, _, state, id := syncServiceFixture(t)
			state.schedule.Mode = mode
			hour := time.Now().UTC().Hour()
			state.schedule.NightStart = (hour + 2) % 24
			state.schedule.NightEnd = (hour + 3) % 24
			require.NoError(t, svc.Step(context.Background(), id))
			require.Equal(t, 1, state.reads)
			require.Zero(t, state.exports)
			require.Zero(t, knowledge.published)
			var root types.VNoteSyncRoot
			require.NoError(t, db.First(&root, "id=?", id).Error)
			require.True(t, root.NextPollAt.After(time.Now().UTC()))
			require.True(t, root.NextPollAt.After(time.Now().UTC().Add(time.Minute)), "pending upserts must not spin outside their model window")
		})
	}
}

func TestVNoteSyncDeletionsDoNotWaitForManualOrNightModelAdmission(t *testing.T) {
	for _, mode := range []string{"manual", "night"} {
		t.Run(mode, func(t *testing.T) {
			svc, db, knowledge, ctx, state, id := syncServiceFixture(t)
			require.NoError(t, svc.Step(context.Background(), id))
			note := syncDesired(t, db, id)
			finishVNoteGeneration(t, db, ctx, note.KnowledgeID)
			wakeSync(t, db, id, false)
			require.NoError(t, svc.Step(context.Background(), id))
			state.schedule.Mode = mode
			hour := time.Now().UTC().Hour()
			state.schedule.NightStart = (hour + 2) % 24
			state.schedule.NightEnd = (hour + 3) % 24
			state.sequence++
			state.deleted = true
			wakeSync(t, db, id, true)
			require.NoError(t, svc.Step(context.Background(), id))
			var published types.VNoteImportState
			require.NoError(t, db.First(&published, "id=?", note.ID).Error)
			require.Empty(t, published.KnowledgeID)
			require.Equal(t, "done", syncDesired(t, db, id).Status)
			require.Equal(t, 1, knowledge.published)
			require.Equal(t, 1, state.exports, "deletion reads no additional body")
		})
	}
}

func TestVNoteSyncPausedRootReadsNoNotesAndResumesWithoutReauthorization(t *testing.T) {
	svc, db, knowledge, _, state, id := syncServiceFixture(t)
	state.paused = true
	require.NoError(t, svc.Step(context.Background(), id))
	require.Zero(t, state.reads)
	require.Zero(t, state.exports)
	require.Zero(t, knowledge.published)
	state.paused = false
	wakeSync(t, db, id, true)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Equal(t, 1, state.exports)
	require.Equal(t, 1, knowledge.published)
}

func TestVNoteDeletedSourceRootEmptiesCopyKeepsKBAndRestoresScopedSync(t *testing.T) {
	svc, db, knowledge, ctx, source, id := syncServiceFixture(t)
	require.NoError(t, svc.Step(context.Background(), id))
	note := syncDesired(t, db, id)
	finishVNoteGeneration(t, db, ctx, note.KnowledgeID)
	wakeSync(t, db, id, false)
	require.NoError(t, svc.Step(context.Background(), id))
	var published types.VNoteImportState
	require.NoError(t, db.First(&published, "id=?", note.ID).Error)
	kbID := published.KnowledgeBaseID
	source.rootMissing = true
	source.graphRevision++
	source.sequence++
	source.schedule.Mode = "manual"
	source.paused = true
	before := source.reads
	wakeSync(t, db, id, true)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Equal(t, before, source.reads, "pause retains copies even if the source root disappears")
	require.NoError(t, db.First(&published, "id=?", note.ID).Error)
	require.Equal(t, note.KnowledgeID, published.KnowledgeID)
	source.paused = false
	for i := 0; i < 3; i++ {
		wakeSync(t, db, id, i == 0)
		require.NoError(t, svc.Step(context.Background(), id))
	}
	require.NoError(t, db.First(&published, "id=?", note.ID).Error)
	require.Empty(t, published.KnowledgeID)
	kb, err := svc.imports.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID)
	require.NoError(t, err)
	require.Equal(t, "V-Note / Notebook", kb.Name, "a missing root cannot erase its retained KB name")
	require.Equal(t, 1, knowledge.published)
	require.Equal(t, 1, source.exports)
	// Restoring the same selected root revalidates its tree and resumes the plan.
	source.rootMissing = false
	source.graphRevision++
	source.sequence++
	source.schedule.Mode = "interval"
	for i := 0; i < 2; i++ {
		wakeSync(t, db, id, true)
		require.NoError(t, svc.Step(context.Background(), id))
	}
	require.Equal(t, 2, knowledge.published)
	require.Equal(t, 2, source.exports)
	require.NotEqual(t, note.Attempt, syncDesired(t, db, id).Attempt)
	var kbs int64
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Count(&kbs).Error)
	require.EqualValues(t, 1, kbs)
}

func TestVNoteAlreadyMissingSourceRootCreatesNoKBOrModelWork(t *testing.T) {
	svc, db, knowledge, _, source, id := syncServiceFixture(t)
	source.rootMissing = true
	source.graphRevision++
	require.NoError(t, svc.Step(context.Background(), id))
	var kbs int64
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Count(&kbs).Error)
	require.Zero(t, kbs)
	require.Zero(t, source.exports)
	require.Zero(t, knowledge.published)
}

func TestVNoteSyncBusyGlobalSlotDoesNotExportAnotherBody(t *testing.T) {
	svc, db, knowledge, ctx, state, id := syncServiceFixture(t)
	require.NoError(t, svc.Step(context.Background(), id))
	wakeSync(t, db, id, false)
	root, err := repository.ClaimVNoteSyncRoot(ctx, db, id, time.Now().UTC())
	require.NoError(t, err)
	note := types.VNoteSyncNote{ID: "second", SyncRootID: id, NoteID: "another", SourceHash: strings.Repeat("b", 64), Title: "Another", Operation: "upsert", Status: "pending", Revision: 1, ScanID: root.ScanID, NextAttemptAt: time.Now().UTC()}
	require.NoError(t, db.Create(&note).Error)
	require.NoError(t, svc.processDesired(ctx, strings.Repeat("a", 64), root, note, state.schedule))
	require.Equal(t, 1, state.exports)
	require.Equal(t, 1, knowledge.published)
}

func TestVNoteSyncRuntimeBootstrapsOnlyAuthorizedRoots(t *testing.T) {
	svc, db, knowledge, ctx, state, id := syncServiceFixture(t)
	require.NoError(t, db.Where("id=?", id).Delete(&types.VNoteSyncRoot{}).Error)
	require.NoError(t, svc.RunOnce(context.Background()))
	require.Equal(t, 1, knowledge.published)
	require.Error(t, svc.imports.ApplyControl(context.Background(), "subject", "personal", 2, false, nil))
	require.NoError(t, svc.imports.ApplyControl(ctx, "subject", "personal", 2, false, nil))
	wakeSync(t, db, id, true)
	before := state.reads
	require.NoError(t, svc.RunOnce(context.Background()))
	require.Equal(t, before, state.reads)
	require.Equal(t, 1, knowledge.published)
}

func TestVNoteSyncStatusDoesNotExposeAnotherOwnerOrWorkspace(t *testing.T) {
	svc, _, _, ctx, _, id := syncServiceFixture(t)
	require.NoError(t, svc.Step(context.Background(), id))
	rows, err := svc.imports.SynchronizationStatus(ctx, "subject", "personal", []string{"root"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "indexing", rows[0].State)
	for _, scope := range []struct{ user, subject, workspace string }{{"bob", "subject", "personal"}, {"alice", "other", "personal"}, {"alice", "subject", "other"}} {
		foreign := types.WithCaller(context.Background(), types.Caller{UserID: scope.user, TenantID: 7, Role: types.TenantRoleOwner})
		rows, err := svc.imports.SynchronizationStatus(foreign, scope.subject, scope.workspace, []string{"root"})
		require.NoError(t, err)
		require.Empty(t, rows)
	}
}

func TestVNoteSyncConfirmedCancellationCanRetryButUnknownModelOutcomeCannot(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed_cancel", true: "unknown"}[unknown], func(t *testing.T) {
			svc, db, knowledge, _, _, id := syncServiceFixture(t)
			require.NoError(t, svc.Step(context.Background(), id))
			old := syncDesired(t, db, id)
			if unknown {
				require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", old.KnowledgeID).Updates(map[string]any{"parse_status": types.ParseStatusProcessing, "error_message": "remote outcome unknown"}).Error)
			} else {
				require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", old.KnowledgeID).Update("parse_status", types.ParseStatusCancelled).Error)
				require.NoError(t, db.Model(&types.VNoteImportGeneration{}).Where("attempt=?", old.Attempt).Update("status", "cancelled").Error)
			}
			wakeSync(t, db, id, false)
			require.NoError(t, svc.Step(context.Background(), id))
			note := syncDesired(t, db, id)
			if unknown {
				require.Equal(t, "blocked", note.Status)
				require.Equal(t, "recovery_required", note.ErrorCode)
			} else {
				require.Equal(t, "pending", note.Status)
				wakeSync(t, db, id, false)
				require.NoError(t, svc.Step(context.Background(), id))
				require.Equal(t, 2, knowledge.published)
				require.NotEqual(t, old.Attempt, syncDesired(t, db, id).Attempt)
			}
		})
	}
}

func TestVNoteSyncRecentEditsSettleBeforeAnyBodyExportOrModelCall(t *testing.T) {
	svc, db, knowledge, _, state, id := syncServiceFixture(t)
	state.snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Zero(t, state.exports)
	require.Zero(t, knowledge.published)
	note := syncDesired(t, db, id)
	require.Equal(t, "pending", note.Status)
	require.True(t, note.NextAttemptAt.After(time.Now().UTC().Add(50*time.Second)))
	// Another edit refreshes the deadline even when the old item was due.
	state.sequence++
	state.snapshot.SourceHash = strings.Repeat("e", 64)
	state.snapshot.Markdown = "latest body"
	state.snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	wakeSync(t, db, id, true)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Zero(t, state.exports)
	require.Zero(t, knowledge.published)
	require.True(t, syncDesired(t, db, id).NextAttemptAt.After(time.Now().UTC().Add(50*time.Second)))
	// Simulate the persisted deadline passing without sleeping or new edits.
	wakeSync(t, db, id, false)
	require.NoError(t, svc.Step(context.Background(), id))
	require.Equal(t, 1, state.exports)
	require.Equal(t, 1, knowledge.published)
}

func TestVNoteStoppedRootStillReportsItsOwnRecoveryWithoutAdvancingQueue(t *testing.T) {
	svc, db, _, ctx, state, id := syncServiceFixture(t)
	require.NoError(t, svc.Step(context.Background(), id))
	note := syncDesired(t, db, id)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", note.KnowledgeID).Updates(map[string]any{"parse_status": types.ParseStatusProcessing, "error_message": "unknown model outcome"}).Error)
	require.NoError(t, svc.imports.ApplyControl(ctx, "subject", "personal", 2, false, []string{"root"}))
	rows, err := svc.imports.SynchronizationStatus(ctx, "subject", "personal", []string{"root"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "attention", rows[0].State)
	require.Equal(t, "recovery_required", rows[0].ErrorCode)
	before := state.exports
	require.NoError(t, svc.RunOnce(context.Background()))
	require.Equal(t, before, state.exports)
}
