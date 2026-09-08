package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contractReviewRouteServiceStub struct {
	interfaces.ContractReviewService
	purged bool
}

func (s *contractReviewRouteServiceStub) DeleteTenantData(context.Context, uint64) error {
	s.purged = true
	return nil
}

func contractReviewRouteTestEngine(t *testing.T, role types.TenantRole, enabled bool, svc *contractReviewRouteServiceStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(7))
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{
			ID:                   7,
			LegalWorkspaceConfig: &types.LegalWorkspaceConfig{Enabled: enabled},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.UserIDContextKey.String(), "user-1")
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Next()
	})
	rv := true
	g := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &rv}}}
	RegisterContractReviewRoutes(r.Group("/api/v1"), handler.NewContractReviewHandler(svc), g)
	return r
}

func TestContractReviewRoutesRequireOwnerForTenantPurge(t *testing.T) {
	svc := &contractReviewRouteServiceStub{}
	r := contractReviewRouteTestEngine(t, types.TenantRoleAdmin, false, svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/legal-workspace-data", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, svc.purged)

	svc = &contractReviewRouteServiceStub{}
	r = contractReviewRouteTestEngine(t, types.TenantRoleOwner, false, svc)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/legal-workspace-data", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, svc.purged, "explicit purge must remain available while access is disabled")
}

func TestContractReviewRoutesRejectDisabledWorkspace(t *testing.T) {
	svc := &contractReviewRouteServiceStub{}
	r := contractReviewRouteTestEngine(t, types.TenantRoleViewer, false, svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/contract-reviews", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, svc.purged)
}
