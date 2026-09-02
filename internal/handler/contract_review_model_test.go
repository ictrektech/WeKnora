package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type contractReviewUpdateHandlerStub struct {
	interfaces.ContractReviewService
	modelID *string
}

func (s *contractReviewUpdateHandlerStub) Update(_ context.Context, _ uint64, _, _, _, _, _ string, modelID *string, _ *bool) (*types.ContractReview, error) {
	s.modelID = modelID
	return &types.ContractReview{ID: "review-1", ModelID: dereferenceContractReviewModelID(modelID)}, nil
}

func dereferenceContractReviewModelID(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestContractReviewHandlerPassesSelectedModelID(t *testing.T) {
	stub := &contractReviewUpdateHandlerStub{}
	h := NewContractReviewHandler(stub)
	r := contractReviewHandlerTestRouter()
	r.PATCH("/contract-reviews/:id", h.Update)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/contract-reviews/review-1", strings.NewReader(`{"model_id":"chat-model-1"}`)))

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, stub.modelID)
	require.Equal(t, "chat-model-1", *stub.modelID)
}
