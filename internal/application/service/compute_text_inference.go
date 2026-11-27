package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxComputeTextTargets          = 8
	defaultComputeBusyMaxRetries   = 5
	defaultComputeBusyBaseBackoff  = 2 * time.Second
	defaultComputeBusyMaxBackoff   = 30 * time.Second
	computeBusyMaxRetriesEnv       = "HYBRAG_COMPUTE_BUSY_MAX_RETRIES"
	computeBusyBaseBackoffMsEnv    = "HYBRAG_COMPUTE_BUSY_BASE_BACKOFF_MS"
	computeBusyMaxBackoffMsEnv     = "HYBRAG_COMPUTE_BUSY_MAX_BACKOFF_MS"
)

// computeTextCaller abstracts remote ComputeRouter.GenerateText for tests.
type computeTextCaller interface {
	GenerateText(ctx context.Context, origin string, req *proto.TextInferenceRequest) (*proto.TextInferenceResponse, error)
}

type computeTextCatalog interface {
	LookupTextModel(origin, modelID string) (docparser.ComputeTextModelInfo, bool)
}

// ComputeTextRetryPolicy controls bounded backoff retries on V-Scheduler
// 429 WAIT_COMPUTE_RESOURCE responses.
type ComputeTextRetryPolicy struct {
	MaxRetries  int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	Sleep       func(context.Context, time.Duration) error
}

func defaultComputeTextRetryPolicy() ComputeTextRetryPolicy {
	maxRetries := envNonNegativeInt(computeBusyMaxRetriesEnv, defaultComputeBusyMaxRetries)
	baseMs := envPositiveInt(computeBusyBaseBackoffMsEnv, int(defaultComputeBusyBaseBackoff/time.Millisecond))
	maxMs := envPositiveInt(computeBusyMaxBackoffMsEnv, int(defaultComputeBusyMaxBackoff/time.Millisecond))
	base := time.Duration(baseMs) * time.Millisecond
	maxB := time.Duration(maxMs) * time.Millisecond
	if maxB < base {
		maxB = base
	}
	return ComputeTextRetryPolicy{
		MaxRetries:  maxRetries,
		BaseBackoff: base,
		MaxBackoff:  maxB,
		Sleep:       sleepWithContext,
	}
}

func envNonNegativeInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ComputeTextFallbackEvent records one transition to a configured fallback
// target when an earlier node was busy or unavailable.
type ComputeTextFallbackEvent struct {
	Node        string `json:"node"`
	ModelID     string `json:"model_id,omitempty"`
	ModelName   string `json:"model_name,omitempty"`
	Reason      string `json:"reason"`
	BusyRetries int    `json:"busy_retries,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ComputeTextResult carries the completed chat response along with Trace
// telemetry identifying the executing node, retries, and fallback history.
type ComputeTextResult struct {
	Response         *types.ChatResponse
	Node             string
	ModelID          string
	ModelName        string
	BusyRetries      int
	RemoteDurationMs int64
	Fallbacks        []ComputeTextFallbackEvent
}

// ComputeTextExecutionError preserves execution node, retry count, fallback
// history, and structured failure reason so Trace can render them on failure.
type ComputeTextExecutionError struct {
	Node        string
	ModelID     string
	ModelName   string
	Reason      string
	BusyRetries int
	Fallbacks   []ComputeTextFallbackEvent
	Err         error
}

func (e *ComputeTextExecutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Node != "" && e.Reason != "" && e.Err != nil {
		return fmt.Sprintf("node %s (%s): %v", e.Node, e.Reason, e.Err)
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Reason
}

func (e *ComputeTextExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ValidateComputeTextRoute normalizes and validates a per-KB compute-node text
// inference route. Targets already present on existingRoute remain valid when a
// paired node is temporarily unreachable so unrelated KB edits can still save.
func ValidateComputeTextRoute(
	ctx context.Context,
	catalog computeTextCatalog,
	models interfaces.ModelService,
	route *types.ComputeTextRouteConfig,
	existingRoute *types.ComputeTextRouteConfig,
) (*types.ComputeTextRouteConfig, error) {
	if route == nil {
		return nil, nil
	}
	if len(route.Targets) == 0 {
		return nil, nil
	}
	if len(route.Targets) > maxComputeTextTargets {
		return nil, werrors.NewBadRequestError(fmt.Sprintf("configure at most %d compute text inference targets", maxComputeTextTargets))
	}

	existingTargets := make(map[string]types.ComputeTextTarget)
	if existingRoute != nil {
		for _, t := range existingRoute.Targets {
			origin := docparser.NormalizeComputeOrigin(t.NodeOrigin)
			id := strings.TrimSpace(t.ModelID)
			if origin != "" {
				existingTargets[origin+"|"+id] = t
			}
		}
	}

	normalized := &types.ComputeTextRouteConfig{
		Targets:         make([]types.ComputeTextTarget, 0, len(route.Targets)),
		FallbackToLocal: route.FallbackToLocal,
	}
	seen := make(map[string]bool, len(route.Targets))
	for _, raw := range route.Targets {
		origin := docparser.NormalizeComputeOrigin(raw.NodeOrigin)
		if origin == "" {
			return nil, werrors.NewBadRequestError("compute text target node_origin must be 'local' or an HTTPS VOS origin")
		}
		modelID := strings.TrimSpace(raw.ModelID)
		modelName := strings.TrimSpace(raw.ModelName)
		key := origin + "|" + modelID
		if seen[key] {
			continue
		}
		seen[key] = true

		if origin == docparser.ComputeNodeLocal {
			if modelID != "" {
				validated, err := ValidateQuestionGenerationModel(ctx, models, modelID)
				if err != nil {
					return nil, err
				}
				modelID = validated
				if m, getErr := models.GetModelByID(ctx, modelID); getErr == nil && m != nil {
					if m.DisplayName != "" {
						modelName = m.DisplayName
					} else {
						modelName = m.Name
					}
				}
			}
		} else {
			if modelID == "" || len(modelID) > types.ModelIDMaxLen {
				return nil, werrors.NewBadRequestError("remote compute text target requires a valid model_id")
			}
			if catalog != nil {
				if info, ok := catalog.LookupTextModel(origin, modelID); ok {
					if info.DisplayName != "" {
						modelName = info.DisplayName
					} else if info.Name != "" {
						modelName = info.Name
					}
				} else if prev, kept := existingTargets[key]; kept {
					if modelName == "" {
						modelName = prev.ModelName
					}
				} else {
					return nil, werrors.NewBadRequestError(fmt.Sprintf(
						"model %s on compute node %s is not authorized or not yet discovered; check the node first",
						modelID, origin,
					))
				}
			} else if _, kept := existingTargets[key]; !kept {
				return nil, werrors.NewBadRequestError("compute node routing is unavailable in this deployment")
			}
		}

		normalized.Targets = append(normalized.Targets, types.ComputeTextTarget{
			NodeOrigin: origin,
			ModelID:    modelID,
			ModelName:  modelName,
		})
	}
	if len(normalized.Targets) == 0 {
		return nil, nil
	}
	return normalized, nil
}

// ExecuteComputeTextInference runs one text inference call according to the
// KB's compute route (or local model when no route is configured), with bounded
// backoff retries on V-Scheduler 429 WAIT_COMPUTE_RESOURCE and strictly
// opt-in fallback to other configured nodes or local model.
func ExecuteComputeTextInference(
	ctx context.Context,
	caller computeTextCaller,
	models interfaces.ModelService,
	route *types.ComputeTextRouteConfig,
	localModelID string,
	messages []chat.Message,
	opts *chat.ChatOptions,
	purpose string,
	policy ComputeTextRetryPolicy,
) (*ComputeTextResult, error) {
	if policy.Sleep == nil {
		policy.Sleep = sleepWithContext
	}
	candidates := buildComputeTextCandidates(route, localModelID)
	if len(candidates) == 0 {
		return nil, &ComputeTextExecutionError{
			Node:   docparser.ComputeNodeLocal,
			Reason: "MODEL_NOT_CONFIGURED",
			Err:    errors.New("no text inference model is configured"),
		}
	}

	var fallbacks []ComputeTextFallbackEvent
	totalBusyRetries := 0
	var lastErr *ComputeTextExecutionError

	for idx, target := range candidates {
		res, busyRetries, execErr := executeComputeTextTargetWithRetry(
			ctx, caller, models, target, messages, opts, purpose, policy,
		)
		totalBusyRetries += busyRetries
		if execErr == nil {
			res.BusyRetries = totalBusyRetries
			res.Fallbacks = fallbacks
			return res, nil
		}
		reason := classifyComputeTextFailureReason(execErr)
		lastErr = &ComputeTextExecutionError{
			Node:        target.NodeOrigin,
			ModelID:     target.ModelID,
			ModelName:   target.ModelName,
			Reason:      reason,
			BusyRetries: totalBusyRetries,
			Fallbacks:   append([]ComputeTextFallbackEvent(nil), fallbacks...),
			Err:         execErr,
		}
		// Never switch away on caller cancellation or malformed input.
		if ctx.Err() != nil || reason == "CANCELLED" || reason == "INVALID_REQUEST" {
			return nil, lastErr
		}
		// Only switch if another node or local fallback was explicitly configured.
		if idx+1 < len(candidates) {
			logger.Warnf(ctx,
				"[ComputeText] purpose=%s target=%s model=%s failed (%s, busy_retries=%d), switching to configured fallback %s: %v",
				purpose, target.NodeOrigin, target.ModelID, reason, busyRetries, candidates[idx+1].NodeOrigin, execErr,
			)
			fallbacks = append(fallbacks, ComputeTextFallbackEvent{
				Node:        target.NodeOrigin,
				ModelID:     target.ModelID,
				ModelName:   target.ModelName,
				Reason:      reason,
				BusyRetries: busyRetries,
				Error:       previewText(execErr.Error(), 240),
			})
			continue
		}
		return nil, lastErr
	}
	return nil, lastErr
}

func isRetryableComputeTextError(err error) bool {
	var execErr *ComputeTextExecutionError
	if !errors.As(err, &execErr) || execErr == nil {
		return false
	}
	switch execErr.Reason {
	case docparser.ComputeErrorCodeBusy, "NODE_UNAVAILABLE", "TIMEOUT", "INFERENCE_FAILED":
		return true
	default:
		return false
	}
}

func buildComputeTextCandidates(route *types.ComputeTextRouteConfig, localModelID string) []types.ComputeTextTarget {
	localModelID = strings.TrimSpace(localModelID)
	if route == nil || len(route.Targets) == 0 {
		return []types.ComputeTextTarget{{
			NodeOrigin: docparser.ComputeNodeLocal,
			ModelID:    localModelID,
		}}
	}
	candidates := make([]types.ComputeTextTarget, 0, len(route.Targets)+1)
	hasLocal := false
	for _, t := range route.Targets {
		origin := docparser.NormalizeComputeOrigin(t.NodeOrigin)
		if origin == "" {
			continue
		}
		modelID := strings.TrimSpace(t.ModelID)
		if origin == docparser.ComputeNodeLocal {
			hasLocal = true
			if modelID == "" {
				modelID = localModelID
			}
			if modelID == "" {
				continue
			}
		} else if modelID == "" {
			continue
		}
		candidates = append(candidates, types.ComputeTextTarget{
			NodeOrigin: origin,
			ModelID:    modelID,
			ModelName:  t.ModelName,
		})
	}
	if route.FallbackToLocal && !hasLocal && localModelID != "" {
		candidates = append(candidates, types.ComputeTextTarget{
			NodeOrigin: docparser.ComputeNodeLocal,
			ModelID:    localModelID,
		})
	}
	return candidates
}

func executeComputeTextTargetWithRetry(
	ctx context.Context,
	caller computeTextCaller,
	models interfaces.ModelService,
	target types.ComputeTextTarget,
	messages []chat.Message,
	opts *chat.ChatOptions,
	purpose string,
	policy ComputeTextRetryPolicy,
) (*ComputeTextResult, int, error) {
	busyRetries := 0
	for attempt := 0; ; attempt++ {
		res, err := executeComputeTextTargetOnce(ctx, caller, models, target, messages, opts, purpose)
		if err == nil {
			return res, busyRetries, nil
		}
		busy, isBusy := docparser.ClassifyComputeBusyError(err)
		if !isBusy || attempt >= policy.MaxRetries || ctx.Err() != nil {
			return nil, busyRetries, err
		}
		delay := computeBusyBackoff(policy, attempt, busy.RetryAfter)
		busyRetries++
		logger.Warnf(ctx,
			"[ComputeText] purpose=%s node=%s model=%s busy (%s), retry %d/%d after %s",
			purpose, target.NodeOrigin, target.ModelID, busy.Code, busyRetries, policy.MaxRetries, delay,
		)
		if sleepErr := policy.Sleep(ctx, delay); sleepErr != nil {
			return nil, busyRetries, sleepErr
		}
	}
}

func computeBusyBackoff(policy ComputeTextRetryPolicy, attempt int, retryAfter time.Duration) time.Duration {
	base := policy.BaseBackoff
	if base <= 0 {
		base = defaultComputeBusyBaseBackoff
	}
	maxB := policy.MaxBackoff
	if maxB < base {
		maxB = base
	}
	delay := base
	for i := 0; i < attempt && delay < maxB; i++ {
		delay *= 2
	}
	if delay > maxB {
		delay = maxB
	}
	if retryAfter > delay {
		delay = retryAfter
		if delay > maxB {
			delay = maxB
		}
	}
	return delay
}

func executeComputeTextTargetOnce(
	ctx context.Context,
	caller computeTextCaller,
	models interfaces.ModelService,
	target types.ComputeTextTarget,
	messages []chat.Message,
	opts *chat.ChatOptions,
	purpose string,
) (*ComputeTextResult, error) {
	if target.NodeOrigin == docparser.ComputeNodeLocal {
		if models == nil {
			return nil, errors.New("local chat model is not configured")
		}
		chatModel, err := models.GetChatModel(ctx, target.ModelID)
		if err != nil {
			return nil, fmt.Errorf("get local chat model: %w", err)
		}
		releaseLLM, err := acquireBackgroundLLMSlot(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to wait for background LLM slot: %w", err)
		}
		defer releaseLLM()
		modelCtx := types.WithLLMCallMetadata(ctx, purpose, "")
		resp, err := chatModel.Chat(modelCtx, messages, opts)
		if err != nil {
			return nil, err
		}
		modelName := target.ModelName
		if modelName == "" && target.ModelID != "" {
			modelName = chatModel.GetModelName()
		}
		return &ComputeTextResult{
			Response:  resp,
			Node:      docparser.ComputeNodeLocal,
			ModelID:   target.ModelID,
			ModelName: modelName,
		}, nil
	}

	if caller == nil {
		return nil, status.Error(codes.Unavailable, "compute router is unavailable")
	}
	protoMessages := make([]*proto.TextChatMessage, 0, len(messages))
	for _, m := range messages {
		protoMessages = append(protoMessages, &proto.TextChatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	req := &proto.TextInferenceRequest{
		ModelId:  target.ModelID,
		Messages: protoMessages,
		Purpose:  purpose,
	}
	if opts != nil {
		req.Temperature = opts.Temperature
		req.MaxTokens = int32(opts.MaxTokens)
		if opts.Thinking != nil && !*opts.Thinking {
			req.DisableThinking = true
		}
	}
	resp, err := caller.GenerateText(ctx, target.NodeOrigin, req)
	if err != nil {
		return nil, err
	}
	modelName := resp.GetModelName()
	if modelName == "" {
		modelName = target.ModelName
	}
	return &ComputeTextResult{
		Response: &types.ChatResponse{
			Content:      resp.GetContent(),
			FinishReason: resp.GetFinishReason(),
			Usage: types.TokenUsage{
				PromptTokens:     int(resp.GetPromptTokens()),
				CompletionTokens: int(resp.GetCompletionTokens()),
				TotalTokens:      int(resp.GetTotalTokens()),
			},
		},
		Node:             target.NodeOrigin,
		ModelID:          target.ModelID,
		ModelName:        modelName,
		RemoteDurationMs: resp.GetDurationMs(),
	}, nil
}

func classifyComputeTextFailureReason(err error) string {
	if err == nil {
		return ""
	}
	if _, busy := docparser.ClassifyComputeBusyError(err); busy {
		return docparser.ComputeErrorCodeBusy
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT"
	}
	if st, ok := status.FromError(err); ok {
		msg := strings.ToLower(st.Message())
		switch st.Code() {
		case codes.ResourceExhausted:
			if strings.Contains(strings.ToUpper(st.Message()), docparser.ComputeErrorCodeBusy) {
				return docparser.ComputeErrorCodeBusy
			}
			return "RESOURCE_EXHAUSTED"
		case codes.PermissionDenied:
			if strings.Contains(msg, "not authorized") {
				return "MODEL_NOT_SHARED"
			}
			return "CAPABILITY_DISABLED"
		case codes.Unauthenticated:
			return "UNAUTHENTICATED"
		case codes.NotFound:
			return "NODE_NOT_PAIRED"
		case codes.InvalidArgument:
			return "INVALID_REQUEST"
		case codes.DeadlineExceeded:
			return "TIMEOUT"
		case codes.Canceled:
			return "CANCELLED"
		case codes.Unavailable:
			return "NODE_UNAVAILABLE"
		}
	}
	return "INFERENCE_FAILED"
}

func attachComputeTextTelemetry(out types.JSONMap, res *ComputeTextResult, err error) {
	if out == nil {
		return
	}
	if res != nil {
		out["compute_node"] = res.Node
		if res.ModelID != "" {
			out["model_id"] = res.ModelID
		}
		if res.ModelName != "" {
			out["compute_model_name"] = res.ModelName
		}
		if res.BusyRetries > 0 {
			out["busy_retries"] = res.BusyRetries
		}
		if res.RemoteDurationMs > 0 {
			out["remote_duration_ms"] = res.RemoteDurationMs
		}
		if len(res.Fallbacks) > 0 {
			out["fallback_from"] = res.Fallbacks
		}
		return
	}
	var execErr *ComputeTextExecutionError
	if errors.As(err, &execErr) {
		if execErr.Node != "" {
			out["compute_node"] = execErr.Node
		}
		if execErr.ModelID != "" {
			out["model_id"] = execErr.ModelID
		}
		if execErr.ModelName != "" {
			out["compute_model_name"] = execErr.ModelName
		}
		if execErr.Reason != "" {
			out["failure_reason"] = execErr.Reason
		}
		if execErr.BusyRetries > 0 {
			out["busy_retries"] = execErr.BusyRetries
		}
		if len(execErr.Fallbacks) > 0 {
			out["fallback_from"] = execErr.Fallbacks
		}
	}
}
