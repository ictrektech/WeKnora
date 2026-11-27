package repository

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDocumentChunkReplacementPreservesOldRowsOnFailure(t *testing.T) {
	db := setupChunkTestDB(t)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	knowledge := &types.Knowledge{ID: "test-reparse", TenantID: 1, ParseStatus: types.ParseStatusProcessing}
	require.NoError(t, db.Create(knowledge).Error)
	repo := &chunkRepository{db: db}
	old := &types.Chunk{ID: "old", TenantID: 1, KnowledgeID: knowledge.ID, IsEnabled: true}
	staged := &types.Chunk{ID: "new", TenantID: 1, KnowledgeID: knowledge.ID, IndexStatus: types.ChunkIndexStatusStaged}
	require.NoError(t, repo.CreateChunks(t.Context(), []*types.Chunk{old, staged}))

	imageInfo := map[string]string{staged.ID: `[{"url":"fixture"}]`}
	err := repo.PublishDocumentChunkReplacement(t.Context(), 1, knowledge.ID, []string{old.ID}, []string{staged.ID, "missing"}, imageInfo)
	require.ErrorIs(t, err, ErrChunkRevisionConflict)
	var rows []*types.Chunk
	require.NoError(t, db.Order("id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, staged.ID, rows[0].ID)
	require.False(t, rows[0].IsEnabled)
	require.Empty(t, rows[0].ImageInfo)
	require.Equal(t, types.ChunkIndexStatusStaged, rows[0].IndexStatus)
	require.Equal(t, old.ID, rows[1].ID)
	require.True(t, rows[1].IsEnabled)

	require.NoError(t, repo.PublishDocumentChunkReplacement(t.Context(), 1, knowledge.ID, []string{old.ID}, []string{staged.ID}, imageInfo))
	rows = nil
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, staged.ID, rows[0].ID)
	require.Equal(t, "ready", rows[0].IndexStatus)
	require.True(t, rows[0].IsEnabled)
	require.Equal(t, imageInfo[staged.ID], rows[0].ImageInfo)
}
