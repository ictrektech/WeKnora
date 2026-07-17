package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

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
6. Retrieved tool results contain Markdown images. Unless the user explicitly requested text-only output or every image is clearly unrelated, the final answer MUST include at least one relevant Markdown image copied verbatim from the tool results. Preserve its complete URL exactly. Use ASCII half-width parentheses exactly as ![alt](url) and never use full-width （ or ）. Place the image immediately after the paragraph it supports. When multiple images support different sections, distribute them across those sections instead of stopping after the first image.
7. Before finishing, silently verify that the answer contains a Markdown image when requirement 6 applies.`
}

// streamFinalAnswerToEventBus streams the final answer generation through EventBus
func (e *AgentEngine) streamFinalAnswerToEventBus(
	ctx context.Context,
	query string,
	state *types.AgentState,
	sessionID string,
) error {
	totalToolCalls := countTotalToolCalls(state.RoundSteps)
	logger.Infof(ctx, "[Agent][FinalAnswer] Synthesizing from %d steps, %d tool calls",
		len(state.RoundSteps), totalToolCalls)
	common.PipelineInfo(ctx, "Agent", "final_answer_start", map[string]interface{}{
		"session_id":   sessionID,
		"query":        query,
		"steps":        len(state.RoundSteps),
		"tool_results": totalToolCalls,
	})

	// Build messages with all context
	systemPrompt := e.buildSystemPrompt(ctx)
	userTurn := e.RenderUserTurnContent(sessionID, query)

	messages := []chat.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userTurn},
	}

	// Add all tool call results as context
	toolResultCount := 0
	hasRetrievedImage := false
	for stepIdx, step := range state.RoundSteps {
		for toolIdx, toolCall := range step.ToolCalls {
			toolResultCount++
			if searchutil.MarkdownImageRegex.MatchString(toolCall.Result.Output) {
				hasRetrievedImage = true
			}
			messages = append(messages, chat.Message{
				Role:    "user",
				Content: fmt.Sprintf("Tool %s returned: %s", toolCall.Name, toolCall.Result.Output),
			})
			logger.Debugf(ctx, "[Agent][FinalAnswer] Added tool result [Step-%d][Tool-%d]: %s (output: %d chars)",
				stepIdx+1, toolIdx+1, toolCall.Name, len(toolCall.Result.Output))
		}
	}

	logger.Debugf(ctx, "[Agent][FinalAnswer] Built context: %d messages, %d tool results",
		len(messages), toolResultCount)

	imageRequirement := finalAnswerImageRequirement(hasRetrievedImage)

	// Add final answer prompt
	finalPrompt := fmt.Sprintf(`Based on the above tool call results, generate a complete answer for the user's question.

User question: %s

Requirements:
1. Answer based on the actually retrieved content
2. Cite sources by document title or name; never expose chunk_id, knowledge_id, tool names, or other internal identifiers
3. Organize the answer in a structured format
4. If information is insufficient, honestly state so
5. IMPORTANT: Respond in the same language as the user's question
%s

Now generate the final answer:`, query, imageRequirement)

	messages = append(messages, chat.Message{
		Role:    "user",
		Content: finalPrompt,
	})
	inputBudget, completionTokens := e.finalAnswerTokenBudgets()
	messages = e.fitFinalAnswerMessages(ctx, messages, inputBudget, completionTokens)

	// Generate a single ID for this entire final answer stream
	answerID := generateEventID("answer")
	logger.Debugf(ctx, "[Agent][FinalAnswer] AnswerID: %s", answerID)
	answerDoneEmitted := false

	llmResult, err := e.streamLLMToEventBus(
		ctx,
		messages,
		&chat.ChatOptions{
			Temperature:         e.config.Temperature,
			MaxCompletionTokens: completionTokens,
		}, // Thinking disabled for final answer synthesis
		func(chunk *types.StreamResponse, fullContent string) {
			// Defensive filter: only emit answer content, skip thinking chunks
			if chunk.ResponseType == types.ResponseTypeThinking {
				return
			}
			if chunk.Content != "" {
				logger.Debugf(ctx, "[Agent][FinalAnswer] Emitting answer chunk: %d chars", len(chunk.Content))
				e.eventBus.Emit(ctx, event.Event{
					ID:        answerID,
					Type:      event.EventAgentFinalAnswer,
					SessionID: sessionID,
					Data: event.AgentFinalAnswerData{
						Content: chunk.Content,
						Done:    chunk.Done,
					},
				})
				if chunk.Done {
					answerDoneEmitted = true
				}
			}
		},
	)
	if err != nil {
		logger.Errorf(ctx, "[Agent][FinalAnswer] Final answer generation failed: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_stream_failed", map[string]interface{}{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return err
	}

	if !answerDoneEmitted {
		e.eventBus.Emit(ctx, event.Event{
			ID:        answerID,
			Type:      event.EventAgentFinalAnswer,
			SessionID: sessionID,
			Data: event.AgentFinalAnswerData{
				Content: "",
				Done:    true,
			},
		})
	}

	// Safety net: strip any residual <think> blocks that may have leaked through
	fullAnswer := agenttools.StripThinkBlocks(llmResult.Content)
	logger.Infof(ctx, "[Agent][FinalAnswer] Final answer generated: %d characters", len(fullAnswer))
	common.PipelineInfo(ctx, "Agent", "final_answer_done", map[string]interface{}{
		"session_id": sessionID,
		"answer_len": len(fullAnswer),
	})
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

// handleMaxIterations generates a final answer when the agent loop exhausted all iterations
// without the LLM producing a natural stop. It marks state.IsComplete = true.
func (e *AgentEngine) handleMaxIterations(
	ctx context.Context, query string, state *types.AgentState, sessionID string,
) {
	logger.Info(ctx, "Reached max iterations, generating final answer")
	common.PipelineWarn(ctx, "Agent", "max_iterations_reached", map[string]interface{}{
		"iterations": state.CurrentRound,
		"max":        e.config.MaxIterations,
	})

	// Stream final answer generation through EventBus
	if err := e.streamFinalAnswerToEventBus(ctx, query, state, sessionID); err != nil {
		logger.Errorf(ctx, "Failed to synthesize final answer: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_failed", map[string]interface{}{
			"error": err.Error(),
		})
		state.FinalAnswer = "Sorry, I was unable to generate a complete answer."
	}
	state.IsComplete = true
}

// emitCompletionEvent emits the EventAgentComplete event with execution summary.
func (e *AgentEngine) emitCompletionEvent(
	ctx context.Context, state *types.AgentState, sessionID, messageID string, startTime time.Time,
) {
	// Convert knowledge refs to interface{} slice for event data
	knowledgeRefsInterface := make([]interface{}, 0, len(state.KnowledgeRefs))
	for _, ref := range state.KnowledgeRefs {
		knowledgeRefsInterface = append(knowledgeRefsInterface, ref)
	}

	e.eventBus.Emit(ctx, event.Event{
		ID:        generateEventID("complete"),
		Type:      event.EventAgentComplete,
		SessionID: sessionID,
		Data: event.AgentCompleteData{
			FinalAnswer:     state.FinalAnswer,
			KnowledgeRefs:   knowledgeRefsInterface,
			AgentSteps:      state.RoundSteps, // Include detailed execution steps for message storage
			TotalSteps:      len(state.RoundSteps),
			TotalDurationMs: time.Since(startTime).Milliseconds(),
			MessageID:       messageID, // Include message ID for proper message update
		},
	})

	logger.Infof(ctx, "Agent execution completed in %d rounds", state.CurrentRound)
}
