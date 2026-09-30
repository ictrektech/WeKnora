package session

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type titleCapturingSessionService struct {
	interfaces.SessionService
	calls int
}

func (s *titleCapturingSessionService) GenerateTitleAsync(
	_ context.Context, session *types.Session, _ string, modelID string, _ *event.EventBus,
) {
	s.calls++
	if modelID != "answer-model" {
		panic("title did not use the answer model")
	}
	session.Title = "generated"
}

func TestSetupSSEStreamDefersTitleUntilAnswerComplete(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     qaMode
		stopped  bool
		empty    bool
		disabled bool
		title    string
		want     int
	}{
		{name: "quick answer", mode: qaModeNormal, want: 1},
		{name: "agent answer", mode: qaModeAgent, want: 1},
		{name: "stopped answer", stopped: true},
		{name: "empty answer", empty: true},
		{name: "disabled title", disabled: true},
		{name: "existing title", title: "existing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			titles := &titleCapturingSessionService{}
			h := &Handler{streamManager: stream.NewMemoryStreamManager(), sessionService: titles}
			req := &qaRequestContext{
				ctx: t.Context(), c: newSteerWiringGinContext(),
				sessionID: "session", requestID: "request", query: "question",
				session:     &types.Session{ID: "session", Title: tc.title},
				customAgent: &types.CustomAgent{ID: "agent"},
				assistantMessage: &types.Message{
					ID: "answer", SessionID: "session", ModelID: " answer-model ", CreatedAt: time.Now(),
				},
			}
			streamCtx := h.setupSSEStream(req, !tc.disabled, tc.mode)
			defer streamCtx.cancel()
			require.Zero(t, titles.calls, "title must not reserve the model before the answer")
			if !tc.empty {
				require.NoError(t, streamCtx.eventBus.Emit(t.Context(), event.Event{
					Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "answer"},
				}))
			}
			require.Zero(t, titles.calls, "streaming a partial answer must not start the title")
			if tc.stopped {
				streamCtx.cancel()
			}
			complete := event.Event{
				Type: event.EventAgentComplete, Data: event.AgentCompleteData{MessageID: "answer"},
			}
			require.NoError(t, streamCtx.eventBus.Emit(t.Context(), complete))
			require.NoError(t, streamCtx.eventBus.Emit(t.Context(), complete))
			require.Equal(t, tc.want, titles.calls, "completion must start at most one title")
			require.Equal(t, tc.title, req.session.Title, "title generation must receive a session copy")
		})
	}
}
