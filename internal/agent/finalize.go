package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/compaction"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	finalAnswerDefaultCompletionTokens = 2048
	finalAnswerCompletionTokensEnv     = "WEKNORA_AGENT_FINAL_ANSWER_MAX_TOKENS"
	finalAnswerContextSafetyTokensEnv  = "WEKNORA_CHAT_CONTEXT_SAFETY_TOKENS"
	finalAnswerContextSafetyTokens     = 768
	finalAnswerMinimumInputBudget      = 512
)

func finalAnswerImageRequirement(hasRetrievedImage bool) string {
	if !hasRetrievedImage {
		return ""
	}
	return `
5. Retrieved tool results contain Markdown images. Unless the user explicitly requested text-only output or every image is clearly unrelated, the final answer MUST include at least one relevant Markdown image copied verbatim from the tool results. Preserve its complete URL exactly. Use ASCII half-width parentheses exactly as ![alt](url) and never use full-width （ or ）. Place the image immediately after the paragraph it supports. When multiple images support different sections, distribute them across those sections instead of stopping after the first image.
	6. Before finishing, silently verify that the answer contains a Markdown image when requirement 5 applies.`
}

// streamFinalAnswerToEventBus streams the final answer generation through EventBus
func (e *AgentEngine) streamFinalAnswerToEventBus(
	ctx context.Context,
	query string,
	state *types.AgentState,
	sessionID string,
	conversation ...[]chat.Message,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	totalToolCalls := countTotalToolCalls(state.RoundSteps)
	logger.Infof(ctx, "[Agent][FinalAnswer] Synthesizing from %d steps, %d tool calls",
		len(state.RoundSteps), totalToolCalls)
	common.PipelineInfo(ctx, "Agent", "final_answer_start", map[string]interface{}{
		"session_id":   sessionID,
		"query":        query,
		"steps":        len(state.RoundSteps),
		"tool_results": totalToolCalls,
	})

	// Prefer the live transcript, including history, images, compaction and
	// steer messages. The optional form keeps older fallback callers source-
	// compatible while they are migrated.
	var messages []chat.Message
	hasRetrievedImage := false
	for _, step := range state.RoundSteps {
		for _, toolCall := range step.ToolCalls {
			if toolCall.Result != nil && searchutil.MarkdownImageRegex.MatchString(toolCall.Result.Output) {
				hasRetrievedImage = true
			}
		}
	}
	if len(conversation) > 0 {
		// Tool output retains its original role and call ID in the live
		// transcript; never promote it into user instructions.
		messages = append([]chat.Message(nil), conversation[0]...)
	} else {
		// Build a complete fallback transcript for older callers that do not
		// pass the live conversation yet.
		systemPrompt := e.buildSystemPrompt(ctx)
		userTurn := e.RenderUserTurnContent(sessionID, query)
		messages = []chat.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userTurn},
		}
		for stepIdx, step := range state.RoundSteps {
			for toolIdx, toolCall := range step.ToolCalls {
				if toolCall.Result == nil {
					continue
				}
				modelOutput := e.modelContext.ModelToolResultForTool(toolCall.Name, toolCall.Result)
				messages = append(messages, chat.Message{
					Role:    "user",
					Content: fmt.Sprintf("Tool %s returned: %s", toolCall.Name, modelOutput),
				})
				logger.Debugf(ctx, "[Agent][FinalAnswer] Added tool result [Step-%d][Tool-%d]: %s (output: %d chars)",
					stepIdx+1, toolIdx+1, toolCall.Name, len(toolCall.Result.Output))
			}
		}
	}
	runtimeNotice := "<runtime_event>{\"source\":\"runtime\",\"type\":\"final_synthesis\",\"message\":\"Tool " +
		"execution has ended for this run. Respond to the current task, including the " +
		"latest user corrections and source restrictions in the conversation. Base " +
		"claims on the evidence actually obtained; distinguish completed work from " +
		"remaining work and explain any missing evidence. Use the user's requested " +
		"language and format. Do not claim that an unperformed action " +
		"succeeded.\"}</runtime_event>"
	runtimeNotice += finalAnswerImageRequirement(hasRetrievedImage)
	inputBudget, completionTokens := e.finalAnswerTokenBudgets()
	if len(conversation) == 0 {
		messages = e.fitFinalAnswerMessages(ctx, messages, inputBudget, completionTokens)
	}
	if e.finalAnswerNeedsCompaction(messages, runtimeNotice) {
		messages, _ = e.compactFinalAnswerContext(ctx, messages, state.CurrentRound+1,
			runtimeNotice, compaction.ReasonThreshold)
	}

	// Generate a single ID for this entire final answer stream
	answerID := generateEventID("answer")
	logger.Debugf(ctx, "[Agent][FinalAnswer] AnswerID: %s", answerID)
	answerStreamed := false
	providerDone := false
	terminalContent := ""
	splitter := agenttools.NewThinkStreamSplitter()

	var llmResult *streamLLMResult
	var err error
	// This phase has its own single overflow retry. The last tool results and
	// the tool-free synthesis request differ from the preceding ReAct round.
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		inputTokens := e.finalAnswerInputTokens(messages, runtimeNotice)
		budget := min(completionTokens, e.clampCompletionBudgetToContext(inputTokens))
		if e.config.MaxContextTokens > 0 && inputTokens+budget > e.config.MaxContextTokens {
			return fmt.Errorf("final answer synthesis cannot fit after compaction: estimated input %d tokens "+
				"plus completion budget %d exceeds context window %d", inputTokens, budget, e.config.MaxContextTokens)
		}
		llmResult, err = e.streamLLMToEventBus(
			ctx,
			messages,
			&chat.ChatOptions{
				RuntimeNotice:       runtimeNotice,
				Temperature:         e.config.Temperature,
				MaxCompletionTokens: budget,
				PromptCacheKey:      sessionID,
				ToolChoice:          "none",
				Thinking:            new(false), // A nil preference can enable the provider's default thinking mode.
			},
			func(chunk *types.StreamResponse, _ string) {
				// Defensive filter: only emit answer content, skip thinking chunks
				if chunk.ResponseType == types.ResponseTypeThinking {
					return
				}
				_, answerContent := splitter.Feed(chunk.Content)
				if splitter.TakeAnswerReset() {
					_ = e.eventBus.Emit(
						ctx,
						event.Event{
							ID: answerID, Type: event.EventAgentFinalAnswer, SessionID: sessionID,
							Data: event.AgentFinalAnswerData{Replace: true},
						},
					)
				}
				if chunk.Done {
					_, tail := splitter.Flush()
					answerContent += tail
					providerDone = true
				}
				// Do not close the answer until the whole stream has succeeded: a
				// later error may require replacing this attempt with a retry.
				if providerDone {
					terminalContent += answerContent
					return
				}
				if answerContent != "" {
					logger.Debugf(ctx, "[Agent][FinalAnswer] Emitting answer chunk: %d chars", len(answerContent))
					_ = e.eventBus.Emit(ctx, event.Event{
						ID:        answerID,
						Type:      event.EventAgentFinalAnswer,
						SessionID: sessionID,
						Data: event.AgentFinalAnswerData{
							Content: answerContent,
						},
					})
					answerStreamed = true
				}
			},
		)
		// A provider can report a full window as a length stop instead of an
		// HTTP/stream error. Reuse the ReAct detector, and account for every
		// measured attempt even when its answer is replaced by recovery.
		overflow := compaction.IsOverflowError(err)
		if llmResult != nil && llmResult.Usage != nil {
			state.TurnUsage.Accumulate(*llmResult.Usage)
			if err == nil && e.responseHitContextLimit(&types.ChatResponse{
				FinishReason: llmResult.FinishReason, Usage: *llmResult.Usage,
			}) {
				overflow = true
				err = fmt.Errorf("final answer synthesis reached the context window: finish=%s prompt=%d completion=%d",
					llmResult.FinishReason, llmResult.Usage.PromptTokens, llmResult.Usage.CompletionTokens)
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err == nil || attempt == 1 || !overflow {
			break
		}
		logger.Warnf(ctx, "[Agent][FinalAnswer] Context overflow; compacting before one retry: %v", err)
		compacted, changed := e.compactFinalAnswerContext(ctx, messages, state.CurrentRound+1,
			runtimeNotice, compaction.ReasonOverflow)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !changed {
			break // Sending the same rejected context again cannot recover.
		}
		messages = compacted
		if answerStreamed {
			_ = e.eventBus.Emit(ctx, event.Event{
				ID: answerID, Type: event.EventAgentFinalAnswer, SessionID: sessionID,
				Data: event.AgentFinalAnswerData{Replace: true},
			})
		}
		answerStreamed, providerDone, terminalContent = false, false, ""
		splitter = agenttools.NewThinkStreamSplitter()
	}
	if err != nil {
		logger.Errorf(ctx, "[Agent][FinalAnswer] Final answer generation failed: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_stream_failed", map[string]interface{}{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return err
	}
	_, tail := splitter.Flush()
	_ = e.eventBus.Emit(ctx, event.Event{
		ID: answerID, Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Content: terminalContent + tail, Done: true},
	})

	// Safety net: strip any residual <think> blocks that may have leaked through
	fullAnswer := agenttools.StripThinkBlocks(llmResult.Content)
	logger.Infof(ctx, "[Agent][FinalAnswer] Final answer generated: %d characters", len(fullAnswer))
	common.PipelineInfo(ctx, "Agent", "final_answer_done", map[string]interface{}{
		"session_id": sessionID,
		"answer_len": len(fullAnswer),
	})
	// Delegated callers must distinguish a synthesized partial answer from a
	// completed deliverable too, including the iteration-budget fallback path.
	if e.collaboration != nil && e.collaboration.Child != nil && isLengthFinishReason(llmResult.FinishReason) {
		state.RoundSteps = append(state.RoundSteps, types.AgentStep{
			Iteration: state.CurrentRound,
			Thought:   fullAnswer, Truncated: true, Timestamp: time.Now(),
		})
	}
	state.FinalAnswer = fullAnswer
	return nil
}

func (e *AgentEngine) fitFinalAnswerMessages(
	ctx context.Context,
	messages []chat.Message,
	inputBudget int,
	completionTokens int,
) []chat.Message {
	if inputBudget <= 0 || len(messages) <= 3 || e.tokenEstimator.EstimateMessages(messages) <= inputBudget {
		return messages
	}

	trimmed := append([]chat.Message(nil), messages...)
	removed := 0
	for len(trimmed) > 3 && e.tokenEstimator.EstimateMessages(trimmed) > inputBudget {
		trimmed = append(trimmed[:2], trimmed[3:]...)
		removed++
	}
	if removed > 0 {
		logger.Warnf(ctx, "[Agent][FinalAnswer] Trimmed %d old tool result(s) to reserve %d completion tokens",
			removed, completionTokens)
		common.PipelineWarn(ctx, "Agent", "final_answer_context_trimmed", map[string]interface{}{
			"removed_tool_results": removed,
			"input_budget":         inputBudget,
			"message_count":        len(trimmed),
			"completion_tokens":    completionTokens,
		})
	}
	return trimmed
}

func (e *AgentEngine) finalAnswerTokenBudgets() (inputBudget int, completionTokens int) {
	limit := envInt("WEKNORA_CHAT_MODEL_CONTEXT_TOKENS", 16384)
	if e.config != nil && e.config.MaxContextTokens > 0 && e.config.MaxContextTokens < limit {
		limit = e.config.MaxContextTokens
	}
	completionTokens = envInt(finalAnswerCompletionTokensEnv, finalAnswerDefaultCompletionTokens)
	if completionTokens <= 0 {
		completionTokens = finalAnswerDefaultCompletionTokens
	}
	safetyTokens := envInt(finalAnswerContextSafetyTokensEnv, finalAnswerContextSafetyTokens)
	if safetyTokens < 0 {
		safetyTokens = 0
	}
	maxCompletionTokens := limit - safetyTokens - finalAnswerMinimumInputBudget
	if maxCompletionTokens < 1 {
		return 0, 1
	}
	if completionTokens > maxCompletionTokens {
		completionTokens = maxCompletionTokens
	}
	return limit - completionTokens - safetyTokens, completionTokens
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// Measure the same handle-encoded conversation sent by streamLLMToEventBus,
// plus the runtime notice appended by the protocol adapter.
func (e *AgentEngine) finalAnswerInputTokens(messages []chat.Message, notice string) int {
	return e.tokenEstimator.EstimateMessages(e.modelContext.EncodeMessages(messages)) +
		e.tokenEstimator.EstimateMessage(&chat.Message{Role: "system", Content: notice})
}

func (e *AgentEngine) finalAnswerInputBudget() int {
	return max(e.config.MaxContextTokens-e.getCompletionTokenBudget()-contextSafetyTokens, 1)
}

func (e *AgentEngine) finalAnswerNeedsCompaction(messages []chat.Message, notice string) bool {
	return e.config.MaxContextTokens > 0 && e.finalAnswerInputTokens(messages, notice) > e.finalAnswerInputBudget()
}

// Reuse normal compaction and checkpoint persistence. A last tool result can
// exceed the entire window on its own; after summarizing older history, trim
// it to the space left by the fixed prompt, notice and completion budget.
func (e *AgentEngine) compactFinalAnswerContext(
	ctx context.Context, messages []chat.Message, round int, notice string, reason compaction.Reason,
) ([]chat.Message, bool) {
	if ctx.Err() != nil || e.config.MaxContextTokens <= 0 {
		return messages, false
	}
	before := e.finalAnswerInputTokens(messages, notice)
	compacted, changed := e.runCompaction(ctx, messages, round, reason)
	if ctx.Err() != nil {
		return messages, false
	}
	inputTokens := e.finalAnswerInputTokens(compacted, notice)
	if inputTokens > e.finalAnswerInputBudget() || (reason == compaction.ReasonOverflow && !changed) {
		toolTokens := 0
		for _, msg := range e.modelContext.EncodeMessages(compacted) {
			if msg.Role == "tool" {
				toolTokens += e.tokenEstimator.EstimateMessage(&msg)
			}
		}
		budget := min(toolResultBudget(e.config.MaxContextTokens),
			max(e.finalAnswerInputBudget()-(inputTokens-toolTokens), 1))
		var trimmed bool
		compacted, trimmed = trimToolResultsToBudget(compacted, e.tokenEstimator, budget)
		if trimmed {
			e.contextRewrites++
			logger.Infof(ctx, "[Agent][FinalAnswer] Trimmed tool results to %d tokens", budget)
		}
	}
	return compacted, e.finalAnswerInputTokens(compacted, notice) < before
}

// handleMaxIterations generates a final answer when the agent loop exhausted all iterations
// without the LLM producing a natural stop. Completion still persists the
// fallback and prior steps on failure, while the error remains observable.
func (e *AgentEngine) handleMaxIterations(
	ctx context.Context, query string, state *types.AgentState, sessionID string, messages []chat.Message,
) error {
	logger.Info(ctx, "Reached max iterations, generating final answer")
	common.PipelineWarn(ctx, "Agent", "max_iterations_reached", map[string]interface{}{
		"iterations": state.CurrentRound,
		"max":        e.config.MaxIterations,
	})

	// Stream final answer generation through EventBus
	if err := e.streamFinalAnswerToEventBus(ctx, query, state, sessionID, messages); err != nil {
		logger.Errorf(ctx, "Failed to synthesize final answer: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_failed", map[string]interface{}{
			"error": err.Error(),
		})
		state.FinalAnswer = "Sorry, I was unable to generate a complete answer."
		state.IsComplete = true
		return fmt.Errorf("failed to synthesize final answer after max iterations: %w", err)
	}
	state.IsComplete = true
	return nil
}

// emitCompletionEvent emits the EventAgentComplete event with execution summary.
func (e *AgentEngine) emitCompletionEvent(
	ctx context.Context, state *types.AgentState, sessionID, messageID string, startTime time.Time,
) {
	steps := state.RoundSteps
	if state.SuspendedStep != nil {
		steps = append(append([]types.AgentStep(nil), steps...), *state.SuspendedStep)
	}
	seen := e.takeBackgroundSeen()
	if len(state.PendingSteerMessages) > 0 || len(state.PendingBackground) > 0 || len(seen) > 0 {
		// A stop or model failure can arrive after delivery but before the next
		// response exists. Preserve that boundary without inventing an answer.
		steps = append(append([]types.AgentStep(nil), steps...), types.AgentStep{
			Iteration:            state.CurrentRound,
			UserMessagesBefore:   state.PendingSteerMessages,
			BackgroundInjections: state.PendingBackground,
			BackgroundSeenIDs:    seen,
		})
	}
	// Convert knowledge refs to interface{} slice for event data
	knowledgeRefsInterface := make([]interface{}, 0, len(state.KnowledgeRefs))
	for _, ref := range state.KnowledgeRefs {
		knowledgeRefsInterface = append(knowledgeRefsInterface, ref)
	}

	data := event.AgentCompleteData{
		Artifacts:       e.collaborationArtifacts(),
		FinalAnswer:     state.FinalAnswer,
		KnowledgeRefs:   knowledgeRefsInterface,
		AgentSteps:      steps,
		Usage:           turnUsage(state),
		TotalSteps:      len(state.RoundSteps),
		TotalDurationMs: time.Since(startTime).Milliseconds(),
		MessageID:       messageID, // Include message ID for proper message update
	}
	// The turn's usage is aggregated only from calls made through this chat
	// model (each round and the final answer), so it is the model the usage
	// belongs to. The message row keeps the requested binding, which can differ
	// when a request override falls back to the agent's model.
	if e.chatModel != nil {
		data.ModelID = e.chatModel.GetModelID()
	}
	if state.Suspended != nil {
		data.Suspended = state.Suspended
	}
	e.eventBus.Emit(ctx, event.Event{
		ID:        generateEventID("complete"),
		Type:      event.EventAgentComplete,
		SessionID: sessionID,
		Data:      data,
	})

	logger.Infof(ctx, "Agent execution completed in %d rounds", state.CurrentRound)
}

// turnUsage returns the turn's aggregated LLM usage, or nil when no round
// reported usage so the field stays absent from the completion event and the
// persisted message alike.
func turnUsage(state *types.AgentState) *types.TokenUsage {
	// A token scale is worth persisting on its own: the next turn's history
	// loading is calibrated by it even when this provider reported no total.
	if state == nil || (state.TurnUsage.TotalTokens == 0 && state.TurnUsage.ContextTokenScale <= 0) {
		return nil
	}
	usage := state.TurnUsage
	return &usage
}
