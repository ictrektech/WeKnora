package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPatentExamplesPreferencePreservedAcrossWorkspaceChanges(t *testing.T) {
	tenant := &types.Tenant{ID: 42, PatentWorkspaceConfig: &types.PatentWorkspaceConfig{Enabled: true}}
	r := newTenantHandlerTestEngine(t, types.TenantRoleAdmin, tenant)
	for _, input := range []struct {
		body              string
		enabled, examples bool
	}{
		{`{"examples_enabled":false}`, true, false},
		{`{"enabled":false}`, false, false},
		{`{"enabled":true}`, true, false},
		{`{"examples_enabled":true}`, true, true},
	} {
		req := httptest.NewRequest(http.MethodPut, "/tenants/kv/patent-workspace-config", strings.NewReader(input.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, input.enabled, tenant.PatentWorkspaceConfig.IsEnabled())
		require.Equal(t, input.examples, tenant.PatentWorkspaceConfig.AreExamplesEnabled())
	}
}

func TestPatentExamplesPreferenceValidationAndPermissions(t *testing.T) {
	for _, role := range []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor, types.TenantRoleAdmin, types.TenantRoleOwner} {
		tenant := &types.Tenant{ID: 42}
		r := newTenantHandlerTestEngine(t, role, tenant)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/tenants/kv/patent-workspace-config", strings.NewReader(`{"examples_enabled":false}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if role == types.TenantRoleViewer || role == types.TenantRoleContributor {
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Nil(t, tenant.PatentWorkspaceConfig)
		} else {
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.False(t, tenant.PatentWorkspaceConfig.AreExamplesEnabled())
		}
	}
	for _, body := range []string{`{}`, `{"examples_enabled":null}`, `{"examples_enabled":"false"}`, `{"enabled":false,"examples_enabled":1}`} {
		tenant := &types.Tenant{ID: 42}
		r := newTenantHandlerTestEngine(t, types.TenantRoleAdmin, tenant)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/tenants/kv/patent-workspace-config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Nil(t, tenant.PatentWorkspaceConfig)
	}
}
