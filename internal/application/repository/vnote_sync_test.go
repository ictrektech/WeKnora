package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func syncFixture(t *testing.T) (*gorm.DB, types.VNoteSyncRoot) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, db.AutoMigrate(&types.VNoteSyncRoot{}, &types.VNoteSyncNote{}, &VNoteControl{}, &types.VNoteImportState{}, &types.VNoteImportGeneration{}, &types.VNoteImportGate{}, &types.Knowledge{}, &types.KnowledgeBase{}))
	ctx := context.Background()
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 1, true, []string{"root"}))
	require.NoError(t, EnsureVNoteSyncRoot(ctx, db, VNoteDelegation{UserID: "alice", WorkspaceID: "space", Subject: "subject", TenantID: 7}, "root", 1))
	root, err := ClaimVNoteSyncRoot(ctx, db, VNoteSyncRootID("alice", "space", "root"), time.Now().UTC())
	require.NoError(t, err)
	return db, *root
}

func TestVNoteDeletionWakeUsesIndexWithLargeUnprocessedBacklog(t *testing.T) {
	db, root := syncFixture(t)
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "sqlite", "000070_vnote_deletion_queue_index.up.sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(body)).Error)
	require.NoError(t, db.Exec("CREATE INDEX vnote_sync_notes_pending ON vnote_sync_notes(sync_root_id,status,next_attempt_at,id)").Error)
	// A manual-only backlog must not be reread/sorted to discover one deletion.
	require.NoError(t, db.Exec(`WITH RECURSIVE ids(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM ids WHERE n<20000)
    INSERT INTO vnote_sync_notes(id,sync_root_id,note_id,operation,status,next_attempt_at)
    SELECT CAST(n AS TEXT),?,CAST(n AS TEXT),'upsert','pending',? FROM ids`, root.ID, time.Now().UTC()).Error)
	require.NoError(t, db.Create(&types.VNoteSyncNote{ID: "delete", SyncRootID: root.ID, NoteID: "deleted", Operation: "delete", Status: "pending", NextAttemptAt: time.Now().UTC()}).Error)
	for _, deadline := range []string{"", " AND next_attempt_at<=?"} {
		var plan []struct{ Detail string }
		args := []any{root.ID, "pending"}
		if deadline != "" {
			args = append(args, time.Now().UTC().Add(time.Minute))
		}
		require.NoError(t, db.Raw("EXPLAIN QUERY PLAN SELECT next_attempt_at FROM vnote_sync_notes WHERE sync_root_id=? AND status=? AND operation='delete'"+deadline+" ORDER BY next_attempt_at,id LIMIT 1", args...).Scan(&plan).Error)
		combined := ""
		for _, step := range plan {
			combined += step.Detail + "\n"
		}
		require.Contains(t, combined, "vnote_sync_notes_deletions")
		require.NotContains(t, combined, "TEMP B-TREE")
		require.NotContains(t, combined, "SCAN vnote_sync_notes")
	}
}

func sourcePage(root types.VNoteSyncRoot, ids ...string) types.VNoteSourcePage {
	page := types.VNoteSourcePage{Protocol: 1, Subject: root.Subject, WorkspaceID: root.WorkspaceID, RootID: root.RootID, ConfigVersion: root.ConfigVersion, SyncIdentity: "identity", RootName: "Root", GraphRevision: 1, SnapshotCursor: 10, SnapshotCursorStamp: strings.Repeat("a", 64), Notes: []types.VNoteSourceNote{}}
	for _, id := range ids {
		page.Notes = append(page.Notes, types.VNoteSourceNote{NoteID: id, NotebookID: "child", Title: id, SourceHash: strings.Repeat("b", 64)})
	}
	if len(ids) > 0 {
		page.AfterNotebookID = "child"
		page.AfterNoteID = ids[len(ids)-1]
	} else {
		page.AfterNotebookID = root.AfterNotebookID
		page.AfterNoteID = root.AfterNoteID
	}
	return page
}
func reloadSync(t *testing.T, db *gorm.DB, root types.VNoteSyncRoot) types.VNoteSyncRoot {
	t.Helper()
	var current types.VNoteSyncRoot
	require.NoError(t, db.First(&current, "id=?", root.ID).Error)
	return current
}

func TestVNoteSyncSnapshotCommitsBookmarksAndDefersAbsence(t *testing.T) {
	db, root := syncFixture(t)
	ctx := context.Background()
	old := types.VNoteSyncNote{ID: types.VNoteLogicalKnowledgeID(vnoteKBID(root), "old"), SyncRootID: root.ID, NoteID: "old", Operation: "upsert", Status: "done", ScanID: "old-scan", Revision: 1, NextAttemptAt: time.Now()}
	require.NoError(t, db.Create(&old).Error)
	first := sourcePage(root, "a")
	first.HasMore = true
	require.NoError(t, StoreVNoteSourcePage(ctx, db, root, first))
	var note types.VNoteSyncNote
	require.NoError(t, db.First(&note, "id=?", old.ID).Error)
	require.Equal(t, "upsert", note.Operation)
	persisted := reloadSync(t, db, root)
	require.Equal(t, "snapshot", persisted.Phase)
	require.Equal(t, "a", persisted.AfterNoteID)
	require.EqualValues(t, 10, persisted.Cursor)
	require.ErrorIs(t, StoreVNoteSourcePage(ctx, db, root, first), ErrVNoteSyncStale)
	last := sourcePage(persisted, "b")
	require.NoError(t, StoreVNoteSourcePage(ctx, db, persisted, last))
	require.Equal(t, "changes", reloadSync(t, db, root).Phase)
	require.NoError(t, db.First(&note, "id=?", old.ID).Error)
	require.Equal(t, "delete", note.Operation)
	require.Equal(t, "pending", note.Status)
}

func TestVNoteSyncChangesCoalesceAndSkipUnchangedMetadata(t *testing.T) {
	db, root := syncFixture(t)
	ctx := context.Background()
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: vnoteKBID(root), TenantID: root.TenantID, PrivateOwnerID: root.UserID, VNoteRootID: root.RootID, Name: "old-name"}).Error)
	require.NoError(t, StoreVNoteSourcePage(ctx, db, root, sourcePage(root, "a")))
	root = reloadSync(t, db, root)
	require.NoError(t, db.Model(&types.VNoteSyncNote{}).Where("sync_root_id=?", root.ID).Update("status", "done").Error)
	page := sourcePage(root)
	page.Events = []types.VNoteSourceNote{{NoteID: "a", NotebookID: "child", Title: "a", SourceHash: strings.Repeat("b", 64), Operation: "upsert", Sequence: 11}}
	page.Cursor = 12
	page.RootName = "Renamed notebook"
	page.CursorStamp = strings.Repeat("c", 64)
	require.NoError(t, StoreVNoteSourcePage(ctx, db, root, page))
	var note types.VNoteSyncNote
	require.NoError(t, db.Where("sync_root_id=? AND note_id=?", root.ID, "a").Take(&note).Error)
	require.Equal(t, "done", note.Status)
	require.EqualValues(t, 1, note.Revision)
	var kb types.KnowledgeBase
	require.NoError(t, db.First(&kb, "id=?", vnoteKBID(root)).Error)
	require.Equal(t, "V-Note / Renamed notebook", kb.Name)
	root = reloadSync(t, db, root)
	page.Cursor = 13
	page.Events[0].Sequence = 13
	page.Events[0].Title = "new title"
	require.NoError(t, StoreVNoteSourcePage(ctx, db, root, page))
	require.NoError(t, db.First(&note, "id=?", note.ID).Error)
	require.Equal(t, "pending", note.Status)
	require.EqualValues(t, 2, note.Revision)
	root = reloadSync(t, db, root)
	page.Cursor = 14
	page.Events = []types.VNoteSourceNote{{NoteID: "a", Operation: "delete", Sequence: 14}}
	require.NoError(t, StoreVNoteSourcePage(ctx, db, root, page))
	require.NoError(t, db.First(&note, "id=?", note.ID).Error)
	require.Equal(t, "delete", note.Operation)
	require.Equal(t, "", note.Title)
	require.EqualValues(t, 3, note.Revision)
}

func TestVNoteSyncExpiredLeaseAndRevokedConfigCannotAdvance(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "revoked"}[revoke], func(t *testing.T) {
			db, root := syncFixture(t)
			ctx := context.Background()
			if revoke {
				require.NoError(t, ApplyVNoteControl(ctx, db, root.UserID, root.WorkspaceID, root.Subject, 2, false, []string{"root"}))
			} else {
				require.NoError(t, db.Model(&root).Update("lease_until", time.Now().UTC().Add(-time.Second)).Error)
			}
			require.ErrorIs(t, StoreVNoteSourcePage(ctx, db, root, sourcePage(root, "a")), ErrVNoteSyncStale)
			var count int64
			require.NoError(t, db.Model(&types.VNoteSyncNote{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestVNoteSyncDeletionHidesOnlyOwnedPublishedPointer(t *testing.T) {
	db, root := syncFixture(t)
	ctx := context.Background()
	kbID := vnoteKBID(root)
	id := types.VNoteLogicalKnowledgeID(kbID, "gone")
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: kbID, TenantID: 7, PrivateOwnerID: root.UserID, VNoteRootID: root.RootID}).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "copy", TenantID: 7, KnowledgeBaseID: kbID, Channel: "vnote", ParseStatus: types.ParseStatusCompleted, Metadata: []byte(`{"vnote_note_id":"gone","vnote_workspace_id":"space"}`)}).Error)
	require.NoError(t, db.Create(&types.VNoteImportState{ID: id, UserID: root.UserID, KnowledgeBaseID: kbID, KnowledgeID: "copy", ContentHash: "hash"}).Error)
	require.NoError(t, StoreVNoteSourcePage(ctx, db, root, sourcePage(root)))
	root = reloadSync(t, db, root)
	var note types.VNoteSyncNote
	require.NoError(t, db.First(&note, "id=?", id).Error)
	require.Equal(t, "delete", note.Operation)
	// A known in-flight request retains the processing slot and defers deletion.
	require.NoError(t, db.Create(&types.Knowledge{ID: "staging", TenantID: 7, KnowledgeBaseID: kbID, ParseStatus: types.ParseStatusProcessing}).Error)
	require.NoError(t, db.Create(&types.VNoteImportGeneration{Attempt: "attempt", StateID: id, KnowledgeID: "staging", UserID: root.UserID}).Error)
	require.NoError(t, db.Create(&types.VNoteImportGate{ID: "index", KnowledgeID: "staging", TenantID: 7, Attempt: "attempt"}).Error)
	require.ErrorIs(t, ApplyVNoteSourceDeletion(ctx, db, root, note), ErrVNoteSyncStale)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", "staging").Update("parse_status", types.ParseStatusCancelled).Error)
	require.NoError(t, ApplyVNoteSourceDeletion(ctx, db, root, note))
	var state types.VNoteImportState
	require.NoError(t, db.First(&state, "id=?", id).Error)
	require.Empty(t, state.KnowledgeID)
	visible, err := (&knowledgeRepository{db: db}).VisibleKnowledgeScopes(ctx, []string{"copy"})
	require.NoError(t, err)
	require.Empty(t, visible)
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", "copy").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestVNoteSyncMalformedPageCannotCauseAbsenceDeletion(t *testing.T) {
	for _, mutate := range []func(*types.VNoteSourcePage){
		func(p *types.VNoteSourcePage) { p.Notes = nil }, func(p *types.VNoteSourcePage) { p.HasMore = true; p.Notes = []types.VNoteSourceNote{} },
		func(p *types.VNoteSourcePage) { p.SnapshotCursorStamp = "bad" }, func(p *types.VNoteSourcePage) { p.AfterNoteID = "missing" },
		func(p *types.VNoteSourcePage) { p.Subject = "another" }, func(p *types.VNoteSourcePage) { p.Notes = append(p.Notes, p.Notes[0]) },
	} {
		db, root := syncFixture(t)
		page := sourcePage(root, "a")
		mutate(&page)
		require.Error(t, StoreVNoteSourcePage(context.Background(), db, root, page))
		var count int64
		require.NoError(t, db.Model(&types.VNoteSyncNote{}).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestVNoteSyncConfigurationChangeKeepsInFlightAttemptForRecovery(t *testing.T) {
	db, root := syncFixture(t)
	ctx := context.Background()
	note := types.VNoteSyncNote{ID: "note", SyncRootID: root.ID, NoteID: "source", Operation: "upsert", Status: "indexing", KnowledgeID: "physical", Attempt: "attempt", Revision: 1, ScanID: root.ScanID, NextAttemptAt: time.Now().UTC()}
	require.NoError(t, db.Create(&note).Error)
	require.NoError(t, ApplyVNoteControl(ctx, db, root.UserID, root.WorkspaceID, root.Subject, 2, true, []string{root.RootID}))
	require.NoError(t, EnsureVNoteSyncRoot(ctx, db, VNoteDelegation{UserID: root.UserID, WorkspaceID: root.WorkspaceID, Subject: root.Subject, TenantID: root.TenantID}, root.RootID, 2))
	require.NoError(t, db.First(&note, "id=?", note.ID).Error)
	require.Equal(t, "indexing", note.Status)
	require.Equal(t, "physical", note.KnowledgeID)
	require.Equal(t, "attempt", note.Attempt)
	require.EqualValues(t, 2, note.Revision)
}
