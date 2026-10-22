package session

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPatentStreamDisablesMemoryBeforeAsyncClone(t *testing.T) {
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting, types.WorkspaceModePlatform, types.WorkspaceModeLegalAssistant} {
		for _, withAgent := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: "/no-agent", true: "/saved-agent"}[withAgent], func(t *testing.T) {
				req := &qaRequestContext{
					ctx: t.Context(), skipSSE: true, sessionID: "session",
					session:          &types.Session{ID: "session", TenantID: 1, WorkspaceMode: mode},
					assistantMessage: &types.Message{ID: "answer", SessionID: "session"},
				}
				if withAgent {
					enabled := true
					req.customAgent = &types.CustomAgent{Config: types.CustomAgentConfig{MemoryEnabled: &enabled}}
				}
				h := &Handler{streamManager: stream.NewMemoryStreamManager()}
				got := h.setupSSEStream(req, false, qaModeAgent)
				t.Cleanup(got.cancel)
				require.Equal(t, !mode.IsPatent(), types.MemoryAllowedForAgent(got.asyncCtx))
			})
		}
	}
}
