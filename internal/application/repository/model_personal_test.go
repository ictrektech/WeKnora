package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelRepositoryIsolatesPersonalModelsByUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Model{}))
	repo := NewModelRepository(db)
	base := types.Model{TenantID: 7, Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceRemote, Status: types.ModelStatusActive}

	shared := base
	shared.ID, shared.Name = "shared", "shared"
	require.NoError(t, repo.Create(context.Background(), &shared))
	userA := base
	userA.ID, userA.Name, userA.OwnerUserID = "user-a-model", "same-endpoint", "user-a"
	require.NoError(t, repo.Create(context.Background(), &userA))
	userB := base
	userB.ID, userB.Name, userB.OwnerUserID = "user-b-model", "same-endpoint", "user-b"
	require.NoError(t, repo.Create(context.Background(), &userB))

	ctxA := context.WithValue(context.Background(), types.UserIDContextKey, "user-a")
	modelsA, err := repo.List(ctxA, 7, "", "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"shared", "user-a-model"}, modelIDs(modelsA))

	ctxB := context.WithValue(context.Background(), types.UserIDContextKey, "user-b")
	modelsB, err := repo.List(ctxB, 7, "", "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"shared", "user-b-model"}, modelIDs(modelsB))

	hidden, err := repo.GetByID(ctxA, 7, "user-b-model")
	require.NoError(t, err)
	require.Nil(t, hidden)
}

func modelIDs(models []*types.Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
