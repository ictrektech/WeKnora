package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/handler/session"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type patentWorkspaceRouteServiceStub struct {
	interfaces.PatentWorkspaceDataService
	purged bool
}

func (s *patentWorkspaceRouteServiceStub) DeleteTenantData(context.Context, uint64) error {
	s.purged = true
	return nil
}

func patentWorkspaceRouteTestEngine(t *testing.T, role types.TenantRole, enabled bool, svc *patentWorkspaceRouteServiceStub, examples ...bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(7))
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		cfg := &types.PatentWorkspaceConfig{Enabled: enabled}
		if len(examples) > 0 {
			cfg.ExamplesEnabled = &examples[0]
		}
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{
			ID:                    7,
			PatentWorkspaceConfig: cfg,
		})
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.UserIDContextKey.String(), "user-1")
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Next()
	})
	rv := true
	g := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &rv}}}
	RegisterPatentWorkspaceRoutes(r.Group("/api/v1"), &session.Handler{}, handler.NewPatentWorkspaceDataHandler(svc), g)
	return r
}

func TestPatentWorkspaceRoutesRequireOwnerForTenantPurge(t *testing.T) {
	svc := &patentWorkspaceRouteServiceStub{}
	r := patentWorkspaceRouteTestEngine(t, types.TenantRoleAdmin, false, svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/patent-workspace-data", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, svc.purged)

	svc = &patentWorkspaceRouteServiceStub{}
	r = patentWorkspaceRouteTestEngine(t, types.TenantRoleOwner, false, svc)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/patent-workspace-data", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, svc.purged, "explicit purge must remain available while access is disabled")
}

func TestPatentWorkspaceRoutesRejectDisabledWorkspace(t *testing.T) {
	svc := &patentWorkspaceRouteServiceStub{}
	r := patentWorkspaceRouteTestEngine(t, types.TenantRoleViewer, false, svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/patent-search/sessions", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, svc.purged)
}

func TestPatentExamplesReadOnlyRoutesAndCaseIsolation(t *testing.T) {
	r := patentWorkspaceRouteTestEngine(t, types.TenantRoleViewer, true, &patentWorkspaceRouteServiceStub{})
	for _, input := range []struct {
		path     string
		status   int
		contains string
	}{
		{"", http.StatusOK, "writing-outline"},
		{"?workspace_mode=patent_search", http.StatusOK, "search-political-qa"},
		{"?workspace_mode=platform", http.StatusBadRequest, "invalid patent workspace mode"},
		{"/writing-outline", http.StatusOK, "architecture.md"},
		{"/writing-outline/documents/architecture", http.StatusOK, "虚构示例"},
		{"/writing-outline/documents/meeting-notes", http.StatusOK, "立项沟通纪要"},
		{"/writing-consistency/documents/architecture", http.StatusNotFound, "example document not found"},
		{"/writing-no-material/documents/architecture", http.StatusNotFound, "example document not found"},
		{"/writing-outline/documents/catalog", http.StatusNotFound, "example document not found"},
		{"/search-political-qa/documents/CN119961390A", http.StatusOK, `"content":""`},
		{"/search-tax-agent/documents/CN119961396A", http.StatusOK, `"publication_date":"2025-05-09"`},
		{"/search-political-qa/documents/CN119961396A", http.StatusNotFound, "example document not found"},
		{"/writing-outline/documents/CN119961390A", http.StatusNotFound, "example document not found"},
		{"/missing", http.StatusNotFound, "patent example not found"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/patent-workspace/examples"+input.path, nil))
		require.Equal(t, input.status, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), input.contains)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/patent-workspace/examples/writing-outline", nil))
		require.Equal(t, http.StatusNotFound, w.Code, "no mutation or continuation route may exist")
	}
}

func TestPatentExamplesGatesApplyToListsDetailsAndDocuments(t *testing.T) {
	for _, workspaceEnabled := range []bool{false, true} {
		for _, examplesEnabled := range []bool{false, true} {
			r := patentWorkspaceRouteTestEngine(t, types.TenantRoleViewer, workspaceEnabled, &patentWorkspaceRouteServiceStub{}, examplesEnabled)
			for _, path := range []string{"", "/writing-outline", "/writing-outline/documents/architecture", "/search-topic", "/search-topic/documents/CN119961390A"} {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/patent-workspace/examples"+path, nil))
				if !workspaceEnabled || !examplesEnabled {
					require.Equal(t, http.StatusForbidden, w.Code)
				} else {
					require.Equal(t, http.StatusOK, w.Code)
				}
			}
		}
	}
}
