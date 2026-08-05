package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserModelDesensitizationIsolatedByUserAndModelID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.UserModelDesensitization{}))
	repo := NewUserModelDesensitizationRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, &types.UserModelDesensitization{
		UserID: "user-a", ModelID: "qa-model", Enabled: true, NER: false,
		BaseURL: "http://desensitize-a:5000",
	}))
	require.NoError(t, repo.Upsert(ctx, &types.UserModelDesensitization{
		UserID: "user-a", ModelID: "vlm-model", Enabled: true, NER: true,
	}))
	require.NoError(t, repo.Upsert(ctx, &types.UserModelDesensitization{
		UserID: "user-b", ModelID: "qa-model", Enabled: false, NER: false,
		BaseURL: "http://desensitize-b:5000",
	}))

	userAQA, err := repo.Get(ctx, "user-a", "qa-model")
	require.NoError(t, err)
	require.True(t, userAQA.Enabled)
	require.False(t, userAQA.NER)
	require.Equal(t, "http://desensitize-a:5000", userAQA.BaseURL)

	userAVLM, err := repo.Get(ctx, "user-a", "vlm-model")
	require.NoError(t, err)
	require.True(t, userAVLM.Enabled)
	require.True(t, userAVLM.NER)

	userBQA, err := repo.Get(ctx, "user-b", "qa-model")
	require.NoError(t, err)
	require.False(t, userBQA.Enabled)
	require.Equal(t, "http://desensitize-b:5000", userBQA.BaseURL)

	unset, err := repo.Get(ctx, "user-b", "vlm-model")
	require.NoError(t, err)
	require.False(t, unset.Enabled)
}
