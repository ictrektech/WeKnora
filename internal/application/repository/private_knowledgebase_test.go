package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPrivateKBRepositoryPreservesOwnerAndHidesFromOtherMembers(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, database.AutoMigrate(&types.KnowledgeBase{}))
	repository := NewKnowledgeBaseRepository(database)
	owner := types.WithCaller(context.Background(), types.Caller{UserID: "alice", TenantID: 7, Role: types.TenantRoleOwner})
	other := types.WithCaller(context.Background(), types.Caller{UserID: "bob", TenantID: 7, Role: types.TenantRoleOwner})
	private := &types.KnowledgeBase{ID: "private", Name: "Private", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}
	require.NoError(t, repository.CreateKnowledgeBase(owner, private))
	require.NoError(t, repository.CreateKnowledgeBase(owner, &types.KnowledgeBase{ID: "ordinary", Name: "Ordinary", TenantID: 7}))
	_, err = repository.GetKnowledgeBaseByID(other, "private")
	require.ErrorIs(t, err, ErrKnowledgeBaseNotFound)
	rows, err := repository.ListKnowledgeBasesByTenantID(other, 7)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "ordinary", rows[0].ID)
	private.PrivateOwnerID = "bob"
	private.VNoteRootID = "changed"
	require.NoError(t, repository.UpdateKnowledgeBase(owner, private))
	stored, err := repository.GetKnowledgeBaseByID(owner, "private")
	require.NoError(t, err)
	require.Equal(t, "alice", stored.PrivateOwnerID)
	require.Equal(t, "root", stored.VNoteRootID)
	require.ErrorIs(t, repository.DeleteKnowledgeBase(other, "private"), ErrKnowledgeBaseNotFound)
	require.NoError(t, database.AutoMigrate(&types.Knowledge{}, &types.VNoteImportState{}, &types.VNoteImportGeneration{}))
	documents := NewKnowledgeRepository(database)
	require.NoError(t, database.Create(&types.Knowledge{ID: "note", TenantID: 7, KnowledgeBaseID: "private", Channel: "vnote", Metadata: []byte(`{"manual":{"content":"secret"}}`)}).Error)
	require.NoError(t, database.Create(&types.VNoteImportState{ID: "state", UserID: "alice", KnowledgeBaseID: "private", KnowledgeID: "note"}).Error)
	_, err = documents.GetKnowledgeByID(other, 7, "note")
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	_, err = documents.GetKnowledgeByIDOnly(other, "note")
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	hidden, err := documents.GetKnowledgeBatch(other, 7, []string{"note"})
	require.NoError(t, err)
	require.Empty(t, hidden)
	_, err = documents.GetKnowledgeByID(owner, 7, "note")
	require.NoError(t, err)
}
