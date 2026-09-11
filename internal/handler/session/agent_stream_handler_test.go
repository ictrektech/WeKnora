package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type answerStreamRecorder struct {
	interfaces.StreamManager
	events []interfaces.StreamEvent
}

func (s *answerStreamRecorder) AppendEvent(
	_ context.Context,
	_, _ string,
	e interfaces.StreamEvent,
) error {
	s.events = append(s.events, e)
	return nil
}

func TestHandleCompleteDoesNotAppendStreamedAnswerAgain(t *testing.T) {
	message := &types.Message{ID: "assistant-1"}
	stream := &answerStreamRecorder{}
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
	stream := &answerStreamRecorder{}
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
