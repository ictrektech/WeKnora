package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Opt-in integration test; each run owns only its random schema. CI/runtime
// credentials are never loaded automatically or printed by this test.
func TestVNoteSyncPostgresMigrationAndReconciliation(t *testing.T) {
	dsn := os.Getenv("VNOTE_SYNC_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires an isolated PostgreSQL test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	schema := "vnote_sync_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; _ = conn.Close() })
	require.NoError(t, db.Exec("SET search_path TO "+schema).Error)
	require.NoError(t, db.AutoMigrate(&VNoteControl{}, &types.KnowledgeBase{}, &types.Knowledge{}, &types.VNoteImportState{}, &types.VNoteImportGeneration{}, &types.VNoteImportGate{}))
	migration := func(name, direction string) {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "versioned", name+"."+direction+".sql"))
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(body)).Error)
	}
	migration("000165_vnote_sync_queue", "up")
	migration("000166_vnote_deletion_queue_index", "up")
	require.True(t, db.Migrator().HasIndex(&types.VNoteSyncNote{}, "vnote_sync_notes_deletions"))
	ctx := context.Background()
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 1, true, []string{"root"}))
	binding := VNoteDelegation{UserID: "alice", WorkspaceID: "space", Subject: "subject", TenantID: 7}
	require.NoError(t, EnsureVNoteSyncRoot(ctx, db, binding, "root", 1))
	root, err := ClaimVNoteSyncRoot(ctx, db, VNoteSyncRootID("alice", "space", "root"), time.Now().UTC())
	require.NoError(t, err)
	_, err = ClaimVNoteSyncRoot(ctx, db, root.ID, time.Now().UTC())
	require.ErrorIs(t, err, ErrVNoteSyncStale)
	// An earlier manual copy absent from the new snapshot must also reconcile.
	kbID := vnoteKBID(*root)
	oldID := types.VNoteLogicalKnowledgeID(kbID, "old")
	scope := types.VNoteRootScope{UserID: "alice", WorkspaceID: "space", Subject: "subject", RootID: "root", TenantID: 7, ConfigVersion: 1}
	kb := &types.KnowledgeBase{ID: kbID, TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}
	require.NoError(t, NewKnowledgeBaseRepository(db).CreateKnowledgeBase(types.WithVNoteRootScope(ctx, scope), kb))
	require.NoError(t, db.Create(&types.Knowledge{ID: "copy", TenantID: 7, KnowledgeBaseID: kbID, Channel: "vnote", Metadata: []byte(`{"vnote_note_id":"old","vnote_workspace_id":"space"}`)}).Error)
	require.NoError(t, db.Create(&types.VNoteImportState{ID: oldID, UserID: "alice", KnowledgeBaseID: kbID, KnowledgeID: "copy", ContentHash: "old-hash"}).Error)
	first := sourcePage(*root, "a")
	first.HasMore = true
	first.RootName = strings.Repeat("笔", 255)
	require.NoError(t, StoreVNoteSourcePage(ctx, db, *root, first))
	var renamed types.KnowledgeBase
	require.NoError(t, db.First(&renamed, "id=?", kbID).Error)
	require.Equal(t, 255, utf8.RuneCountInString(renamed.Name))
	require.True(t, utf8.ValidString(renamed.Name))
	*root = reloadSync(t, db, *root)
	require.NoError(t, StoreVNoteSourcePage(ctx, db, *root, sourcePage(*root, "b")))
	*root = reloadSync(t, db, *root)
	var tombstone types.VNoteSyncNote
	require.NoError(t, db.First(&tombstone, "id=?", oldID).Error)
	require.Equal(t, "delete", tombstone.Operation)
	require.NoError(t, ApplyVNoteSourceDeletion(ctx, db, *root, tombstone))
	var state types.VNoteImportState
	require.NoError(t, db.First(&state, "id=?", oldID).Error)
	require.Empty(t, state.KnowledgeID)
	page := sourcePage(*root)
	page.Cursor = 11
	page.CursorStamp = strings.Repeat("d", 64)
	page.Events = []types.VNoteSourceNote{{NoteID: "a", NotebookID: "child", Title: "changed", SourceHash: strings.Repeat("e", 64), Operation: "upsert", Sequence: 11}}
	require.NoError(t, StoreVNoteSourcePage(ctx, db, *root, page))
	*root = reloadSync(t, db, *root)
	var changed types.VNoteSyncNote
	require.NoError(t, db.Where("sync_root_id=? AND note_id=?", root.ID, "a").Take(&changed).Error)
	require.EqualValues(t, 2, changed.Revision)
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 2, false, []string{"root"}))
	require.ErrorIs(t, RenameVNoteKnowledgeBase(ctx, db, scope, kb, "Late rename"), ErrVNoteControlStale)
	require.ErrorIs(t, UpdateVNoteSyncNote(ctx, db, *root, changed, "done", "copy", "attempt", "", time.Now().UTC()), ErrVNoteSyncStale)
	migration("000166_vnote_deletion_queue_index", "down")
	migration("000165_vnote_sync_queue", "down")
	require.False(t, db.Migrator().HasTable(&types.VNoteSyncRoot{}))
	require.True(t, db.Migrator().HasTable(&types.Knowledge{}))
}
