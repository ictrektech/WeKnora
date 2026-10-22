package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type patentCreateSessions struct {
	interfaces.SessionService
	created *types.Session
}

func (s *patentCreateSessions) CreateSession(_ context.Context, session *types.Session) (*types.Session, error) {
	s.created = session
	session.ID = "patent"
	return session, nil
}
func TestPatentCreationFixesWorkspaceMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		t.Run(string(mode), func(t *testing.T) {
			sessions := &patentCreateSessions{}
			h := &Handler{sessionService: sessions}
			r := gin.New()
			r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				c.Set(types.TenantIDContextKey.String(), uint64(1))
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.UserIDContextKey, "alice"))
				c.Next()
			})
			if mode == types.WorkspaceModePatentSearch {
				r.POST("/create", h.CreatePatentSearchSession)
			} else {
				r.POST("/create", h.CreatePatentWritingSession)
			}
			for _, body := range []string{`{}`, `{"workspace_mode":"platform"}`} {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/create", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				if strings.Contains(body, "platform") {
					require.Equal(t, http.StatusBadRequest, w.Code)
				} else {
					require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
					require.Equal(t, mode, sessions.created.WorkspaceMode)
					require.Equal(t, "alice", sessions.created.UserID)
				}
			}
		})
	}
}
