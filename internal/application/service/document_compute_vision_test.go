package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type sharedVisionModels struct {
	interfaces.ModelService
	lookupTenant uint64
	lookupID     string
	model        *types.Model
	vision       vlm.VLM
}

func (s *sharedVisionModels) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	s.lookupTenant = types.MustTenantIDFromContext(ctx)
	s.lookupID = id
	return s.model, nil
}

func (s *sharedVisionModels) GetVLMModel(_ context.Context, _ string) (vlm.VLM, error) {
	return s.vision, nil
}

func TestSharedDocumentVisionUsesProviderModel(t *testing.T) {
	stub := &stubVLM{}
	models := &sharedVisionModels{
		model: &types.Model{Type: types.ModelTypeVLLM}, vision: stub,
	}
	factory := SharedDocumentImageAnalyzer(models)
	analyze, err := factory(types.WithExecutionTenant(t.Context(), 99), docparser.ComputeDocumentModel{TenantID: 42, ModelID: "provider-vlm"})
	require.NoError(t, err)
	require.Equal(t, uint64(42), models.lookupTenant)
	require.Equal(t, "provider-vlm", models.lookupID)

	analysis, err := analyze(t.Context(), testPNG(t), true, "中文")
	require.NoError(t, err)
	require.Equal(t, "stubbed answer", analysis.OCRText)
	require.Equal(t, "stubbed answer", analysis.Description)
	require.GreaterOrEqual(t, analysis.OCRDurationMs, int64(0))
	require.GreaterOrEqual(t, analysis.DescriptionDurationMs, int64(0))
	require.Len(t, stub.prompts, 2)
	require.Equal(t, vlmOCRScannedPDFPrompt, stub.prompts[0])
	require.True(t, strings.Contains(stub.prompts[1], "中文"))

	analysis, err = analyze(t.Context(), testPNG(t), false, "中文")
	require.NoError(t, err)
	require.Empty(t, analysis.OCRText)
	require.Len(t, stub.prompts, 3)

	models.model.Type = types.ModelTypeEmbedding
	_, err = factory(t.Context(), docparser.ComputeDocumentModel{TenantID: 42, ModelID: "wrong-model"})
	require.Error(t, err)
}
