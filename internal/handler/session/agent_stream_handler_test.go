package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// capturingStreamManager records every appended event so a handler's output
// can be asserted without a real stream backend. Embedding the interface keeps
// everything else nil-panicky so an un-stubbed call fails loudly.
type capturingStreamManager struct {
	interfaces.StreamManager
	events []interfaces.StreamEvent
}

func (s *capturingStreamManager) AppendEvent(
	_ context.Context, _, _ string, evt interfaces.StreamEvent,
) error {
	s.events = append(s.events, evt)
	return nil
}

func TestHandleCompleteDoesNotAppendStreamedAnswerAgain(t *testing.T) {
	message := &types.Message{ID: "assistant-1"}
	stream := &capturingStreamManager{}
	handler := NewAgentStreamHandler(
		context.Background(),
		"session-1",
		message.ID,
		"request-1",
		1,
		message.CreatedAt,
		message,
		stream,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	err := handler.handleFinalAnswer(context.Background(), event.Event{
		ID: "answer-1",
		Data: event.AgentFinalAnswerData{
			Content: "answer",
		},
	})
	require.NoError(t, err)

	err = handler.handleComplete(context.Background(), event.Event{
		Data: event.AgentCompleteData{
			MessageID:   message.ID,
			FinalAnswer: "answer",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "answer", message.Content)
}

func TestHandleCompleteUsesFinalAnswerWhenNoAnswerWasStreamed(t *testing.T) {
	message := &types.Message{ID: "assistant-1"}
	stream := &capturingStreamManager{}
	handler := NewAgentStreamHandler(
		context.Background(),
		"session-1",
		message.ID,
		"request-1",
		1,
		message.CreatedAt,
		message,
		stream,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	err := handler.handleComplete(context.Background(), event.Event{
		Data: event.AgentCompleteData{
			MessageID:   message.ID,
			FinalAnswer: "fallback answer",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "fallback answer", message.Content)
}

// A failed tool execution is still a tool result: the SSE event must keep
// response_type=tool_result with success=false in its metadata, because
// response_type=error is reserved for internal agent failures (handleError).
func TestAgentStreamHandlerToolResultFailureKeepsToolResultType(t *testing.T) {
	streams := &capturingStreamManager{}
	h := NewAgentStreamHandler(
		context.Background(), "sess-1", "msg-1", "req-1", 1, time.Now(),
		&types.Message{}, streams, nil, nil, nil, nil, nil,
	)

	err := h.handleToolResult(context.Background(), event.Event{
		ID: "evt-1", Type: event.EventAgentToolResult,
		Data: event.AgentToolResultData{
			ToolName:   "shell_exec",
			ToolCallID: "call-1",
			Success:    false,
			Error:      "exit status 1",
		},
	})
	require.NoError(t, err)
	require.Len(t, streams.events, 1)

	evt := streams.events[0]
	require.Equal(t, types.ResponseTypeToolResult, evt.Type)
	require.Equal(t, false, evt.Data["success"])
	require.Equal(t, "exit status 1", evt.Data["error"])
	require.Equal(t, "call-1", evt.Data["tool_call_id"])
}

// Internal error events keep response_type=error with done=true, so clients
// can still distinguish a crashed run from a failed tool call.
func TestAgentStreamHandlerInternalErrorKeepsErrorType(t *testing.T) {
	streams := &capturingStreamManager{}
	h := NewAgentStreamHandler(
		context.Background(), "sess-1", "msg-1", "req-1", 1, time.Now(),
		&types.Message{}, streams, nil, nil, nil, nil, nil,
	)

	err := h.handleError(context.Background(), event.Event{
		ID: "evt-err", Type: event.EventError,
		Data: event.ErrorData{Stage: "think", Error: "llm unavailable"},
	})
	require.NoError(t, err)
	require.Len(t, streams.events, 1)

	evt := streams.events[0]
	require.Equal(t, types.ResponseTypeError, evt.Type)
	require.True(t, evt.Done)
}
