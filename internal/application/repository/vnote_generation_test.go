package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestVNoteGenerationPublishIsFencedAndKeepsOldPointer(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "publish", true: "revoked"}[revoked], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			connection.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = connection.Close() })
			require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeBase{}, &types.VNoteImportState{}, &types.VNoteImportGate{}, &types.VNoteImportGeneration{}, &VNoteControl{}))
			ctx := context.Background()
			require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}).Error)
			require.NoError(t, db.Create(&types.VNoteImportState{ID: "state", UserID: "alice", KnowledgeBaseID: "kb", KnowledgeID: "old", ContentHash: "old-hash"}).Error)
			require.NoError(t, db.Create(&types.Knowledge{ID: "old", TenantID: 7, KnowledgeBaseID: "kb", Channel: "vnote"}).Error)
			doc := &types.Knowledge{ID: "new", TenantID: 7, KnowledgeBaseID: "kb", Channel: "vnote", ParseStatus: types.ParseStatusProcessing, Metadata: []byte(`{"vnote_attempt":"a","vnote_workspace_id":"space","vnote_subject":"subject","vnote_config_version":"1"}`)}
			require.NoError(t, db.Create(doc).Error)
			require.NoError(t, db.Create(&types.VNoteImportGate{ID: "index", KnowledgeID: "new", TenantID: 7, Attempt: "a", ContentHash: "new-hash", LeaseUntil: time.Now()}).Error)
			require.NoError(t, db.Create(&types.VNoteImportGeneration{Attempt: "a", StateID: "state", KnowledgeID: "new", ExpectedKnowledgeID: "old", UserID: "alice", WorkspaceID: "space", Subject: "subject", RootID: "root", ConfigVersion: 1, ContentHash: "new-hash", Status: "staged"}).Error)
			hidden, err := (&knowledgeRepository{db: db}).HiddenVNoteGenerationIDs(ctx, []string{"old", "new", "legacy"})
			require.NoError(t, err)
			require.Equal(t, map[string]bool{"new": true, "legacy": true}, hidden)
			repo := &knowledgeRepository{db: db}
			_, err = repo.GetKnowledgeByID(ctx, 7, "new")
			require.ErrorIs(t, err, ErrKnowledgeNotFound)
			_, err = repo.GetKnowledgeByID(types.WithVNoteAdmission(ctx, "a"), 7, "new")
			require.NoError(t, err)
			require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 1, true, []string{"root"}))
			if revoked {
				require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 2, false, []string{"root"}))
			}
			doc.ParseStatus = types.ParseStatusCompleted
			err = PublishVNoteGeneration(ctx, db, "a", "old", doc)
			var state types.VNoteImportState
			require.NoError(t, db.First(&state, "id = ?", "state").Error)
			var stored types.Knowledge
			require.NoError(t, db.First(&stored, "id = ?", "new").Error)
			if revoked {
				require.ErrorIs(t, err, ErrVNoteGenerationSuperseded)
				require.Equal(t, "old", state.KnowledgeID)
				require.Equal(t, "old-hash", state.ContentHash)
				require.Equal(t, types.ParseStatusProcessing, stored.ParseStatus)
				require.NoError(t, db.Where("attempt = ?", "a").Delete(&types.VNoteImportGeneration{}).Error)
				hidden, err = repo.HiddenVNoteGenerationIDs(ctx, []string{"old", "new", "orphan-vector"})
				require.NoError(t, err)
				require.Equal(t, map[string]bool{"new": true, "orphan-vector": true}, hidden)
				_, err = repo.GetKnowledgeByID(ctx, 7, "new")
				require.ErrorIs(t, err, ErrKnowledgeNotFound)
			} else {
				require.NoError(t, err)
				require.Equal(t, "new", state.KnowledgeID)
				require.Equal(t, "new-hash", state.ContentHash)
				require.Equal(t, types.ParseStatusCompleted, stored.ParseStatus)
				hidden, err = (&knowledgeRepository{db: db}).HiddenVNoteGenerationIDs(ctx, []string{"old", "new", "legacy"})
				require.NoError(t, err)
				require.Equal(t, map[string]bool{"old": true, "legacy": true}, hidden)
				_, err = repo.GetKnowledgeByID(ctx, 7, "new")
				require.NoError(t, err)
				resolved, err := repo.GetKnowledgeByID(ctx, 7, "state")
				require.NoError(t, err)
				require.Equal(t, "new", resolved.ID)
				resolved, err = repo.GetKnowledgeByID(ctx, 7, "old")
				require.NoError(t, err)
				require.Equal(t, "new", resolved.ID)
			}
		})
	}
}

func TestVNoteGenerationCreationRejectsRevokedControl(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeBase{}, &types.VNoteImportState{}, &types.VNoteImportGate{}, &types.VNoteImportGeneration{}, &VNoteControl{}))
	ctx := context.Background()
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}).Error)
	require.NoError(t, db.Create(&types.VNoteImportState{ID: "state", UserID: "alice", KnowledgeBaseID: "kb"}).Error)
	require.NoError(t, db.Create(&types.VNoteImportGate{ID: "index", KnowledgeID: "new", TenantID: 7, Attempt: "a", ContentHash: "hash", LeaseUntil: time.Now()}).Error)
	require.NoError(t, db.Create(&types.VNoteImportGeneration{Attempt: "a", StateID: "state", KnowledgeID: "new", UserID: "alice", WorkspaceID: "space", Subject: "subject", RootID: "root", ConfigVersion: 1, ContentHash: "hash", Status: "staged"}).Error)
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 1, true, []string{"root"}))
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 2, false, []string{"root"}))
	doc := &types.Knowledge{ID: "new", TenantID: 7, KnowledgeBaseID: "kb", Channel: "vnote", Metadata: []byte(`{"vnote_attempt":"a","vnote_workspace_id":"space","vnote_subject":"subject","vnote_root_id":"root","vnote_config_version":"1","vnote_content_hash":"hash"}`)}
	err = (&knowledgeRepository{db: db}).CreateKnowledge(types.WithVNoteAdmission(ctx, "a"), doc)
	require.ErrorIs(t, err, ErrVNoteGenerationSuperseded)
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", "new").Count(&count).Error)
	require.Zero(t, count)
}
