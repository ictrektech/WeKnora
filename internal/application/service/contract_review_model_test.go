package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type contractReviewModelStub struct {
	interfaces.ModelService
	model         *types.Model
	getModelErr   error
	chatModel     chat.Chat
	chatModelID   string
	contextTenant uint64
}

func (s *contractReviewModelStub) GetModelByID(ctx context.Context, _ string) (*types.Model, error) {
	s.contextTenant = types.MustTenantIDFromContext(ctx)
	return s.model, s.getModelErr
}

func (s *contractReviewModelStub) GetChatModel(ctx context.Context, modelID string) (chat.Chat, error) {
	s.contextTenant = types.MustTenantIDFromContext(ctx)
	s.chatModelID = modelID
	return s.chatModel, nil
}

func TestContractReviewCanPinAndClearModel(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	svc.models = &contractReviewModelStub{model: &types.Model{
		ID: "chat-model-1", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive,
	}}
	review, err := svc.Create(context.Background(), 7, "u1")
	require.NoError(t, err)

	selected := " chat-model-1 "
	updated, err := svc.Update(context.Background(), 7, "u1", review.ID, "", "", "", &selected, nil)
	require.NoError(t, err)
	require.Equal(t, "chat-model-1", updated.ModelID)

	clear := ""
	updated, err = svc.Update(context.Background(), 7, "u1", review.ID, "", "", "", &clear, nil)
	require.NoError(t, err)
	require.Empty(t, updated.ModelID)
}

func TestContractReviewRejectsNonChatModel(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	svc.models = &contractReviewModelStub{model: &types.Model{
		ID: "embedding-1", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive,
	}}
	review, err := svc.Create(context.Background(), 7, "u1")
	require.NoError(t, err)

	selected := "embedding-1"
	_, err = svc.Update(context.Background(), 7, "u1", review.ID, "", "", "", &selected, nil)
	require.ErrorIs(t, err, ErrContractReviewInvalidModel)
}

func TestContractReviewUsesPinnedModelForWorker(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	modelService := &contractReviewModelStub{model: &types.Model{
		ID: "chat-model-1", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive,
	}}
	svc.models = modelService
	review := &types.ContractReview{TenantID: 7, UserID: "u1", ModelID: "chat-model-1"}

	_, _, err := svc.resolveReviewModel(context.Background(), review)
	require.NoError(t, err)
	require.Equal(t, "chat-model-1", modelService.chatModelID)
	require.Equal(t, uint64(7), modelService.contextTenant)
}
