package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/models/imageprep"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func ResolveDocumentComputeVisionModel(ctx context.Context, models interfaces.ModelService, binding docparser.ComputeDocumentModel) (vlm.VLM, error) {
	ctx = types.WithExecutionTenant(ctx, binding.TenantID)
	model, err := models.GetModelByID(ctx, binding.ModelID)
	if err != nil {
		return nil, err
	}
	if model == nil || (model.Type != types.ModelTypeVLLM &&
		(model.Type != types.ModelTypeKnowledgeQA || !model.Parameters.SupportsVision)) {
		return nil, errors.New("select an available vision model")
	}
	return models.GetVLMModel(ctx, binding.ModelID)
}

func SharedDocumentImageAnalyzer(models interfaces.ModelService) docparser.DocumentImageAnalyzerFactory {
	return func(ctx context.Context, binding docparser.ComputeDocumentModel) (docparser.DocumentImageAnalyzer, error) {
		model, err := ResolveDocumentComputeVisionModel(ctx, models, binding)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, image []byte, scanned bool, language string) (docparser.DocumentImageAnalysisResult, error) {
			return analyzeSharedDocumentImage(types.WithExecutionTenant(ctx, binding.TenantID), model, image, scanned, language)
		}, nil
	}
}

func analyzeSharedDocumentImage(ctx context.Context, model vlm.VLM, image []byte, scanned bool, language string) (docparser.DocumentImageAnalysisResult, error) {
	var result docparser.DocumentImageAnalysisResult
	if _, err := imageprep.Prepare(image, imageprep.Limits{}); err != nil {
		return result, fmt.Errorf("validate source image: %w", err)
	}
	release, err := acquireBackgroundLLMSlot(ctx)
	if err != nil {
		return result, err
	}
	defer release()

	thinking := false
	opts := &vlm.PredictOptions{Thinking: &thinking}
	images := [][]byte{image}
	if scanned {
		ocrStart := time.Now()
		raw, err := model.PredictWithOptions(types.WithLLMCallMetadata(ctx, "image_ocr", ""),
			images, buildVLMOCRPrompt("scanned_pdf", types.VLMConfig{}), opts)
		result.OCRDurationMs = time.Since(ocrStart).Milliseconds()
		if err != nil {
			return result, fmt.Errorf("recognize scanned page: %w", err)
		}
		ocr, err := validateOCRText(raw)
		if err != nil {
			return result, err
		}
		result.OCRText = ocr
	}
	descStart := time.Now()
	description, err := model.PredictWithOptions(types.WithLLMCallMetadata(ctx, "image_caption", ""), images,
		buildVLMCaptionPrompt(ctx, types.VLMConfig{DescriptionLanguage: language}), opts)
	result.DescriptionDurationMs = time.Since(descStart).Milliseconds()
	if err != nil {
		return result, fmt.Errorf("describe PDF image: %w", err)
	}
	description = strings.TrimSpace(description)
	if description == "" {
		return result, errors.New("the vision model returned an empty image description")
	}
	result.Description = description
	return result, nil
}
