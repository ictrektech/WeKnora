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

func TestVNoteProcessingClaimsAndSubmissionFences(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeBase{}, &types.VNoteImportGate{}, &VNoteControl{}))
	repo := &knowledgeRepository{db: db}
	ctx := context.Background()
	knowledge := &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusPending, Metadata: []byte(`{"vnote_attempt":"attempt-2","vnote_workspace_id":"space","vnote_subject":"subject","vnote_config_version":"1"}`)}
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}).Error)
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 1, true, []string{"root"}))
	require.NoError(t, repo.CreateKnowledge(ctx, knowledge))
	require.NoError(t, db.Create(&types.VNoteImportGate{ID: "index", KnowledgeID: "doc", TenantID: 7, ContentHash: "hash", Attempt: "attempt-2", LeaseUntil: time.Now()}).Error)
	claimed, err := repo.BeginVNoteProcessing(ctx, "doc", "hash", "attempt-1")
	require.NoError(t, err)
	require.False(t, claimed, "superseded job must not start cleanup or embedding")
	claimed, err = repo.BeginVNoteProcessing(ctx, "doc", "wrong", "attempt-2")
	require.NoError(t, err)
	require.False(t, claimed)
	claimed, err = repo.BeginVNoteProcessing(ctx, "doc", "hash", "attempt-2")
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.BeginVNoteProcessing(ctx, "doc", "hash", "attempt-2")
	require.NoError(t, err)
	require.False(t, claimed, "duplicate delivery must not run a second parser")
	knowledge.Title = "stale write"
	require.Error(t, repo.UpdateKnowledge(types.WithVNoteAdmission(ctx, "attempt-1"), knowledge))
	stored, err := repo.GetKnowledgeByID(ctx, 7, "doc")
	require.NoError(t, err)
	require.Empty(t, stored.Title)
	require.Equal(t, types.ParseStatusProcessing, stored.ParseStatus)
	stored.Title = "current write"
	require.NoError(t, repo.UpdateKnowledge(types.WithVNoteAdmission(ctx, "attempt-2"), stored))
	require.NoError(t, db.Model(&types.VNoteImportGate{}).Where("id = ?", "index").Update("attempt", "attempt-3").Error)
	require.Error(t, repo.UpdateKnowledge(types.WithVNoteAdmission(ctx, "attempt-2"), stored))
}

func TestVNoteRevocationFencesQueuedClaimAndStaleWrite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeBase{}, &types.VNoteImportGate{}, &VNoteControl{}))
	ctx := context.Background()
	repo := &knowledgeRepository{db: db}
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}).Error)
	doc := &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusPending, Metadata: []byte(`{"vnote_attempt":"a","vnote_workspace_id":"space","vnote_subject":"subject","vnote_config_version":"1"}`)}
	require.NoError(t, db.Create(doc).Error)
	require.NoError(t, db.Create(&types.VNoteImportGate{ID: "index", KnowledgeID: "doc", TenantID: 7, ContentHash: "hash", Attempt: "a", LeaseUntil: time.Now()}).Error)
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 1, true, []string{"root"}))
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 2, false, []string{"root"}))
	claimed, err := repo.BeginVNoteProcessing(ctx, "doc", "hash", "a")
	require.NoError(t, err)
	require.False(t, claimed)
	var after types.Knowledge
	require.NoError(t, db.First(&after, "id = ?", "doc").Error)
	require.Equal(t, types.ParseStatusCancelled, after.ParseStatus)
	doc.Title = "stale"
	require.Error(t, repo.UpdateKnowledge(types.WithVNoteAdmission(ctx, "a"), doc))
}

func TestVNoteRevocationDoesNotReleaseRunningAttempt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeBase{}, &types.VNoteImportGate{}, &VNoteControl{}))
	ctx := context.Background()
	repo := &knowledgeRepository{db: db}
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}).Error)
	doc := &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusPending, Metadata: []byte(`{"vnote_attempt":"a","vnote_workspace_id":"space","vnote_subject":"subject","vnote_config_version":"1"}`)}
	require.NoError(t, db.Create(doc).Error)
	require.NoError(t, db.Create(&types.VNoteImportGate{ID: "index", KnowledgeID: "doc", TenantID: 7, ContentHash: "hash", Attempt: "a", LeaseUntil: time.Now()}).Error)
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 1, true, []string{"root"}))
	claimed, err := repo.BeginVNoteProcessing(ctx, "doc", "hash", "a")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, repo.CheckVNoteProcessing(ctx, "doc", "hash", "a"))
	require.NoError(t, ApplyVNoteControl(ctx, db, "alice", "space", "subject", 2, false, []string{"root"}))
	require.Error(t, repo.CheckVNoteProcessing(ctx, "doc", "hash", "a"))
	claimed, err = repo.BeginVNoteProcessing(ctx, "doc", "hash", "a")
	require.NoError(t, err)
	require.False(t, claimed)
	var after types.Knowledge
	require.NoError(t, db.First(&after, "id = ?", "doc").Error)
	require.Equal(t, types.ParseStatusProcessing, after.ParseStatus)
	require.Error(t, repo.UpdateKnowledge(types.WithVNoteAdmission(ctx, "a"), doc))
}
