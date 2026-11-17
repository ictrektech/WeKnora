package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type vnoteReadUsers struct {
	interfaces.UserService
	user *types.User
}

func (s *vnoteReadUsers) GetPlatformIdentityUser(_ context.Context, _ string, subject string) (*types.User, error) {
	if subject != "owner-sub" {
		return nil, errors.New("unbound")
	}
	return s.user, nil
}

func TestVNotePrivateReadsRequireCurrentHumanProofButNotActiveSync(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enabled := true
	subject := "owner-sub"
	calls := 0
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer human-proof" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": subject, "enable": enabled})
	}))
	defer platform.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL+"/v1000/oauth2/userinfo")
	t.Setenv("HYBRAG_VOS_SSO_ENABLED", "true")
	users := &vnoteReadUsers{user: &types.User{ID: "alice", TenantID: 7, IsActive: true}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{UserID: "alice", TenantID: 7, Role: types.TenantRoleOwner})
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.Use(VNotePrivateReads(users))
	router.GET("/ordinary", func(c *gin.Context) {
		require.True(t, access.CanReadPrivateKB(c.Request.Context(), &types.KnowledgeBase{ID: "ordinary", TenantID: 7}))
		c.Status(204)
	})
	router.GET("/retained-copy", func(c *gin.Context) {
		require.Empty(t, c.GetHeader(VOSUserProofHeader), "human proof must not reach a downstream proxy")
		kb := &types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}
		for i := 0; i < 5; i++ {
			if !access.CanReadPrivateKB(c.Request.Context(), kb) {
				c.Status(404)
				return
			}
		}
		c.Status(204)
	})
	request := func(path, proof string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if proof != "" {
			req.Header.Set(VOSUserProofHeader, proof)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out.Code
	}
	require.Equal(t, 204, request("/ordinary", ""))
	require.Zero(t, calls)
	require.Equal(t, 404, request("/retained-copy", ""))
	require.Zero(t, calls)
	// No synchronization grant is supplied or queried. An active owner may read
	// a copy retained after stopping synchronization.
	require.Equal(t, 204, request("/retained-copy", "human-proof"))
	require.Equal(t, 1, calls)
	enabled = false
	require.Equal(t, 404, request("/retained-copy", "human-proof"))
	require.Equal(t, 2, calls)
	enabled = true
	subject = "someone-else"
	require.Equal(t, 404, request("/retained-copy", "human-proof"))
	subject = "owner-sub"
	users.user = &types.User{ID: "bob", TenantID: 7, IsActive: true}
	require.Equal(t, 404, request("/retained-copy", "human-proof"))
	users.user = &types.User{ID: "alice", TenantID: 7, IsActive: false}
	require.Equal(t, 404, request("/retained-copy", "human-proof"))
	require.Equal(t, 404, request("/retained-copy", "restricted-grant"))
}

func TestVNotePrivateReadsRejectServiceProofWithoutCallingPlatform(t *testing.T) {
	calls := 0
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer platform.Close()
	t.Setenv("HYBRAG_VOS_OIDC_USERINFO_URL", platform.URL+"/v1000/oauth2/userinfo")
	t.Setenv("HYBRAG_VOS_SSO_ENABLED", "true")
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), types.Caller{UserID: "alice", TenantID: 7}))
		c.Next()
	})
	router.Use(VNotePrivateReads(&vnoteReadUsers{}))
	router.GET("/private", func(c *gin.Context) {
		require.False(t, access.CanReadPrivateKB(c.Request.Context(), &types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}))
		c.Status(204)
	})
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set(VOSUserProofHeader, "e30.eyJhcHBsaWNhdGlvbiI6ImFwcCJ9.signature")
	router.ServeHTTP(httptest.NewRecorder(), req)
	require.Zero(t, calls)
}
