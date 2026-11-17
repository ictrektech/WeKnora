package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type vnoteVisibilityRepo struct {
	interfaces.KnowledgeRepository
	err error
}

func (r vnoteVisibilityRepo) HiddenVNoteGenerationIDs(_ context.Context, ids []string) (map[string]bool, error) {
	if r.err != nil {
		return nil, r.err
	}
	return map[string]bool{"staged": true, "orphan": true}, nil
}

func (r vnoteVisibilityRepo) VisibleKnowledgeScopes(_ context.Context, ids []string) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return map[string]string{"old": "vnote", "other": "ordinary", "outside": "unauthorized"}, nil
}

func (r vnoteVisibilityRepo) ResolveVNoteKnowledgeID(_ context.Context, id string) (string, error) {
	if id == "logical" {
		return "old", nil
	}
	return id, nil
}

func (r vnoteVisibilityRepo) PublicVNoteKnowledgeIDs(_ context.Context, ids []string) (map[string]string, error) {
	return map[string]string{"old": "logical"}, r.err
}

func TestVNoteSearchFiltersUnpublishedBeforeFusion(t *testing.T) {
	svc := &knowledgeBaseService{kgRepo: vnoteVisibilityRepo{}}
	results := []*types.RetrieveResult{{Results: []*types.IndexWithScore{{KnowledgeID: "old", KnowledgeBaseID: "vnote"}, {KnowledgeID: "staged", KnowledgeBaseID: "vnote"}, {KnowledgeID: "orphan", KnowledgeBaseID: "vnote"}, {KnowledgeID: "other", KnowledgeBaseID: "ordinary"}, {KnowledgeID: "old"}, {KnowledgeID: "old", KnowledgeBaseID: "ordinary"}, {KnowledgeID: "outside", KnowledgeBaseID: "unauthorized"}, {}}}}
	kbs := []*types.KnowledgeBase{{ID: "vnote", VNoteRootID: "root"}, {ID: "ordinary"}}
	require.NoError(t, svc.hideUnpublishedVNoteHits(context.Background(), kbs, results))
	require.Len(t, results[0].Results, 2)
	require.Equal(t, "old", results[0].Results[0].KnowledgeID)
	require.Equal(t, "other", results[0].Results[1].KnowledgeID)
	svc.kgRepo = vnoteVisibilityRepo{err: errors.New("database down")}
	require.Error(t, svc.hideUnpublishedVNoteHits(context.Background(), kbs, results))
}

func TestVNoteChunkAccessRejectsUnpublishedGeneration(t *testing.T) {
	svc := &chunkService{knowledgeRepo: vnoteVisibilityRepo{}}
	require.NoError(t, svc.requireVisibleVNoteGeneration(context.Background(), "old"))
	require.ErrorIs(t, svc.requireVisibleVNoteGeneration(context.Background(), "staged"), ErrChunkNotFound)
	svc.knowledgeRepo = vnoteVisibilityRepo{err: errors.New("database down")}
	require.Error(t, svc.requireVisibleVNoteGeneration(context.Background(), "old"))
}

func TestVNoteChunkUsesStablePublicDocumentID(t *testing.T) {
	svc := &chunkService{knowledgeRepo: vnoteVisibilityRepo{}}
	resolved, err := svc.resolveVNoteKnowledgeID(context.Background(), "logical")
	require.NoError(t, err)
	require.Equal(t, "old", resolved)
	chunk := &types.Chunk{ID: "chunk", KnowledgeID: "old"}
	require.NoError(t, svc.setPublicVNoteKnowledgeIDs(context.Background(), []*types.Chunk{chunk}))
	data, err := json.Marshal(chunk)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(data, &body))
	require.Equal(t, "logical", body["knowledge_id"])
	require.Equal(t, "old", chunk.KnowledgeID)
}
