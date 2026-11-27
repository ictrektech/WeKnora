package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const computeTextConcurrencyEnv = "HYBRAG_COMPUTE_TEXT_CONCURRENCY"

// ResolveComputeTextModel verifies that binding points to an active chat model
// in the provider's workspace.
func ResolveComputeTextModel(
	ctx context.Context,
	models interfaces.ModelService,
	binding docparser.ComputeTextModel,
) (*types.Model, error) {
	if models == nil {
		return nil, errors.New("model service is unavailable")
	}
	ctx = types.WithExecutionTenant(ctx, binding.TenantID)
	model, err := models.GetModelByID(ctx, binding.ModelID)
	if err != nil {
		return nil, err
	}
	if model == nil || model.Type != types.ModelTypeKnowledgeQA || model.Status != types.ModelStatusActive {
		return nil, errors.New("select an active chat model")
	}
	return model, nil
}

type sharedTextInferenceExecutor struct {
	models      interfaces.ModelService
	concurrency int
	mu          sync.Mutex
	gates       map[string]chan struct{}
}

// SharedTextInferenceExecutor adapts the provider's workspace chat models
// (including local V-Scheduler or compatible model services) for cross-node
// text inference. Per-model serialization (default concurrency 1) prevents
// concurrent ingestion requests (such as summary and question generation) from
// self-contending for a single local GPU lease.
func SharedTextInferenceExecutor(models interfaces.ModelService) docparser.ComputeTextExecutor {
	return &sharedTextInferenceExecutor{
		models:      models,
		concurrency: envPositiveInt(computeTextConcurrencyEnv, 1),
		gates:       make(map[string]chan struct{}),
	}
}

func (e *sharedTextInferenceExecutor) acquireModelSlot(ctx context.Context, modelID string) (func(), error) {
	if e.concurrency <= 0 {
		return func() {}, nil
	}
	e.mu.Lock()
	sem, ok := e.gates[modelID]
	if !ok {
		sem = make(chan struct{}, e.concurrency)
		e.gates[modelID] = sem
	}
	e.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *sharedTextInferenceExecutor) ListModels(
	ctx context.Context,
	bindings []docparser.ComputeTextModel,
) ([]docparser.ComputeTextModelInfo, error) {
	out := make([]docparser.ComputeTextModelInfo, 0, len(bindings))
	for _, binding := range bindings {
		model, err := ResolveComputeTextModel(ctx, e.models, binding)
		if err != nil {
			continue
		}
		out = append(out, docparser.ComputeTextModelInfo{
			ID:            model.ID,
			Name:          model.Name,
			DisplayName:   model.DisplayName,
			ContextWindow: model.Parameters.ContextWindow,
		})
	}
	return out, nil
}

func (e *sharedTextInferenceExecutor) Generate(
	ctx context.Context,
	binding docparser.ComputeTextModel,
	req *proto.TextInferenceRequest,
) (*proto.TextInferenceResponse, error) {
	model, err := ResolveComputeTextModel(ctx, e.models, binding)
	if err != nil {
		return nil, fmt.Errorf("shared text model unavailable: %w", err)
	}
	execCtx := types.WithExecutionTenant(ctx, binding.TenantID)
	execCtx = types.WithBackgroundTask(execCtx)
	chatModel, err := e.models.GetChatModel(execCtx, binding.ModelID)
	if err != nil {
		return nil, fmt.Errorf("initialize shared chat model: %w", err)
	}

	releaseModel, err := e.acquireModelSlot(execCtx, binding.ModelID)
	if err != nil {
		return nil, err
	}
	defer releaseModel()

	releaseBG, err := acquireBackgroundLLMSlot(execCtx)
	if err != nil {
		return nil, err
	}
	defer releaseBG()

	messages := make([]chat.Message, 0, len(req.GetMessages()))
	for _, m := range req.GetMessages() {
		messages = append(messages, chat.Message{
			Role:    strings.TrimSpace(m.GetRole()),
			Content: m.GetContent(),
		})
	}
	opts := &chat.ChatOptions{
		Temperature: req.GetTemperature(),
		MaxTokens:   int(req.GetMaxTokens()),
	}
	if req.GetDisableThinking() {
		thinking := false
		opts.Thinking = &thinking
	}
	purpose := strings.TrimSpace(req.GetPurpose())
	if purpose == "" {
		purpose = "compute_text_inference"
	}
	callCtx := types.WithLLMCallMetadata(execCtx, purpose, "")

	start := time.Now()
	resp, err := chatModel.Chat(callCtx, messages, opts)
	elapsedMs := time.Since(start).Milliseconds()
	if err != nil {
		if busy, ok := docparser.ClassifyComputeBusyError(err); ok {
			return nil, busy
		}
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("shared chat model returned an empty response")
	}
	return &proto.TextInferenceResponse{
		Content:          resp.Content,
		FinishReason:     resp.FinishReason,
		PromptTokens:     int32(resp.Usage.PromptTokens),
		CompletionTokens: int32(resp.Usage.CompletionTokens),
		TotalTokens:      int32(resp.Usage.TotalTokens),
		DurationMs:       elapsedMs,
		ModelName:        model.Name,
	}, nil
}
