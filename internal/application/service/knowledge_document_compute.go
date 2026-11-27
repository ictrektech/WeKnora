package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

func supportsDocumentComputeImages(config types.EffectiveProcessConfig) bool {
	if !config.EnableMultimodel {
		return true
	}
	if config.ImagePipelineID != types.ImagePipelineDefault || config.ImageAttrsEnabled ||
		config.ImageVectorEnabled || config.VLMConfig.CustomInstructions != "" {
		return false
	}
	pipeline := defaultPipeline{}
	for _, field := range pipeline.Fields() {
		if field.Type != types.ImageFieldTypeBool {
			continue
		}
		if value, exists := config.ImagePipelineParams[types.SlotKey(string(pipeline.ID()), field.Key)]; exists {
			actual, ok := value.(bool)
			if !ok || actual != field.Default {
				return false
			}
		}
	}
	return true
}

func (s *knowledgeService) computeLocalPDFImages(ctx context.Context, tenantID uint64, modelID string,
	result *types.ReadResult, options *types.DocumentComputeOptions,
) error {
	if !options.RecognizeImages || len(result.ImageRefs) == 0 {
		return nil
	}
	if s.modelService == nil || modelID == "" {
		return errors.New("a local vision model is required for document processing")
	}
	analyze, err := SharedDocumentImageAnalyzer(s.modelService)(ctx, docparser.ComputeDocumentModel{TenantID: tenantID, ModelID: modelID})
	if err != nil {
		return err
	}
	sources, err := docparser.ReadPDFImageSources(result.Metadata)
	if err != nil {
		return err
	}
	for i := range result.ImageRefs {
		image := &result.ImageRefs[i]
		scanned := sources.Scanned(image.OriginalRef, image.Filename)
		analysis, err := analyze(ctx, image.ImageData, scanned, options.DescriptionLanguage)
		if err != nil {
			return err
		}
		image.Analysis = &types.DocumentImageAnalysis{
			OCRText:               analysis.OCRText,
			Description:           analysis.Description,
			OCRPerformed:          scanned,
			OCRDurationMs:         analysis.OCRDurationMs,
			DescriptionDurationMs: analysis.DescriptionDurationMs,
		}
	}
	return nil
}

func (s *knowledgeService) removePreviousDocumentIndexes(ctx context.Context,
	remove func(context.Context, []string, int, string) error,
	knowledge *types.Knowledge, currentModel embedding.Embedder, previous []string,
) {
	var dim int
	if currentModel != nil {
		dim = currentModel.GetDimensions()
	}
	if knowledge.EmbeddingModelID != "" && s.modelService != nil {
		if model, err := s.modelService.GetEmbeddingModel(ctx, knowledge.EmbeddingModelID); err == nil && model != nil {
			dim = model.GetDimensions()
		}
	}
	if dim != 0 {
		if err := remove(ctx, previous, dim, knowledge.Type); err != nil {
			logger.Warnf(ctx, "Clean up replaced indexes for %s: %v", knowledge.ID, err)
		}
	}
}

func (s *knowledgeService) recordDocumentComputeRecognition(ctx context.Context, knowledgeID string,
	images []docparser.StoredImage, metadata map[string]string,
) {
	node := metadata["docreader_node"]
	if node == "" {
		node = docparser.ComputeNodeLocal
	}
	s.beginStage(ctx, knowledgeID, types.StageMultimodal, nil)
	tracker := s.tracker()
	parent := tracker.LookupStage(ctx, knowledgeID, attemptFromCtx(ctx), types.StageMultimodal)
	timed, hasTimed := tracker.(timedSpanRecorder)

	var totalMeasuredMs int64
	for _, image := range images {
		if image.Analysis == nil {
			continue
		}
		if image.Analysis.OCRPerformed && image.Analysis.OCRDurationMs > 0 {
			totalMeasuredMs += image.Analysis.OCRDurationMs
		}
		if image.Analysis.DescriptionDurationMs > 0 {
			totalMeasuredMs += image.Analysis.DescriptionDurationMs
		}
	}
	stageEnd := time.Now()
	stageStart := stageEnd.Add(-time.Duration(totalMeasuredMs) * time.Millisecond)
	cursor := stageStart

	ocrCount, descriptionCount := 0, 0
	for i, image := range images {
		if image.Analysis == nil {
			continue
		}
		if image.Analysis.OCRPerformed {
			ocrCount++
			if parent != nil {
				durMs := image.Analysis.OCRDurationMs
				if durMs < 0 {
					durMs = 0
				}
				out := types.JSONMap{
					"compute_node":         node,
					"status":               "completed",
					"executor_duration_ms": durMs,
				}
				if hasTimed {
					timed.RecordCompletedSubSpan(ctx, parent, fmt.Sprintf("multimodal.image[%d].ocr", i), types.SpanKindGeneration, nil, out, cursor, durMs)
					cursor = cursor.Add(time.Duration(durMs) * time.Millisecond)
				} else {
					span := tracker.BeginSubSpan(ctx, parent, fmt.Sprintf("multimodal.image[%d].ocr", i), types.SpanKindGeneration, nil)
					tracker.EndSpan(ctx, span, out)
				}
			}
		}
		descriptionCount++
		if parent != nil {
			durMs := image.Analysis.DescriptionDurationMs
			if durMs < 0 {
				durMs = 0
			}
			out := types.JSONMap{
				"compute_node":         node,
				"status":               "completed",
				"executor_duration_ms": durMs,
			}
			if hasTimed {
				timed.RecordCompletedSubSpan(ctx, parent, fmt.Sprintf("multimodal.image[%d].caption", i), types.SpanKindGeneration, nil, out, cursor, durMs)
				cursor = cursor.Add(time.Duration(durMs) * time.Millisecond)
			} else {
				span := tracker.BeginSubSpan(ctx, parent, fmt.Sprintf("multimodal.image[%d].caption", i), types.SpanKindGeneration, nil)
				tracker.EndSpan(ctx, span, out)
			}
		}
	}
	stageOut := types.JSONMap{
		"compute_node": node, "docreader_node": node, "docreader_strategy": metadata["docreader_strategy"],
		"ocr_images": ocrCount, "described_images": descriptionCount,
		"executor_duration_ms": totalMeasuredMs,
	}
	if hasTimed && parent != nil {
		timed.EndSpanWithTiming(ctx, parent, stageOut, stageStart, totalMeasuredMs)
		return
	}
	s.endStage(ctx, knowledgeID, types.StageMultimodal, stageOut)
}

func documentComputeImageChunks(knowledge *types.Knowledge, images []docparser.StoredImage, chunks []types.ParsedChunk, seq int) []*types.Chunk {
	var result []*types.Chunk
	now := time.Now()
	for i, image := range images {
		if image.Analysis == nil {
			continue
		}
		info := types.ImageInfo{
			URL: image.ServingURL, OriginalURL: image.ServingURL,
			OCRText: image.Analysis.OCRText, Caption: image.Analysis.Description,
		}
		setImageSourceAttrs(&info, &types.ImageMultimodalPayload{ImageIndex: i, SourceLocators: image.SourceLocators})
		metadata, _ := json.Marshal([]types.ImageInfo{info})
		owner := imageChunkOwner(image.ServingURL, chunks)
		for _, part := range []struct {
			kind types.ChunkType
			text string
		}{
			{types.ChunkTypeImageOCR, info.OCRText},
			{types.ChunkTypeImageCaption, info.Caption},
		} {
			if part.text == "" {
				continue
			}
			result = append(result, &types.Chunk{
				ID: uuid.NewString(), TenantID: knowledge.TenantID, KnowledgeID: knowledge.ID,
				KnowledgeBaseID: knowledge.KnowledgeBaseID, ChunkIndex: seq, ChunkType: part.kind,
				Content: part.text, ParentChunkID: owner, SourceLocators: image.SourceLocators,
				IsEnabled: true, Flags: types.ChunkFlagRecommended, ImageInfo: string(metadata),
				CreatedAt: now, UpdatedAt: now,
			})
			seq++
		}
	}
	return result
}

type chunkReplacementPublisher interface {
	PublishDocumentChunkReplacement(context.Context, uint64, string, []string, []string, map[string]string) error
}

type documentChunkReplacement struct {
	publisher chunkReplacementPublisher
	previous  []string
	staged    []string
	imageInfo map[string]string
	published bool
}

func (s *knowledgeService) prepareDocumentChunkReplacement(ctx context.Context, knowledge *types.Knowledge) (*documentChunkReplacement, error) {
	publisher, ok := s.chunkRepo.(chunkReplacementPublisher)
	if !ok {
		return nil, errors.New("document chunk replacement is unavailable")
	}
	previous, err := s.chunkRepo.ListAllChunksByKnowledgeID(ctx, knowledge.TenantID, knowledge.ID)
	if err != nil {
		return nil, fmt.Errorf("list previous document chunks: %w", err)
	}
	replacement := &documentChunkReplacement{publisher: publisher}
	for _, chunk := range previous {
		if chunk.IndexStatus != types.ChunkIndexStatusStaged {
			replacement.previous = append(replacement.previous, chunk.ID)
		}
	}
	return replacement, nil
}

func (r *documentChunkReplacement) stage(chunks []*types.Chunk) {
	r.imageInfo = make(map[string]string)
	for _, chunk := range chunks {
		r.staged = append(r.staged, chunk.ID)
		chunk.IsEnabled = false
		chunk.IndexStatus = types.ChunkIndexStatusStaged
		// Publish gallery entries with the chunks, after indexing succeeds.
		if chunk.ImageInfo != "" {
			r.imageInfo[chunk.ID] = chunk.ImageInfo
			chunk.ImageInfo = ""
		}
	}
}

func (s *knowledgeService) rollbackDocumentChunkReplacement(ctx context.Context, r *documentChunkReplacement,
	knowledge *types.Knowledge, deleteIndex func(context.Context, []string, int, string) error, dimension int,
) {
	if r.published || len(r.staged) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if deleteIndex != nil {
		if err := deleteIndex(ctx, r.staged, dimension, knowledge.Type); err != nil {
			logger.Warnf(ctx, "Clean up staged indexes for %s: %v", knowledge.ID, err)
		}
	}
	if err := s.chunkRepo.DeleteChunks(ctx, knowledge.TenantID, r.staged); err != nil {
		logger.Warnf(ctx, "Clean up staged chunks for %s: %v", knowledge.ID, err)
	}
}
