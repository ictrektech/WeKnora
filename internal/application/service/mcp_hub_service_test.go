package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/mcphub"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMCPHubRefreshPreservesSettingsAndDisablesNewTools(t *testing.T) {
	remote := []map[string]any{{
		"name": "search", "enabled": true, "status": "connected",
		"config": map[string]any{"description": "Search tools"},
		"tools":  []any{map[string]any{"name": "query", "inputSchema": map[string]any{"type": "object"}}},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer delegated-token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": remote})
	}))
	t.Cleanup(server.Close)

	hubClient, err := mcphub.NewClientWithBaseURL(server.URL)
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open("file:mcp-hub-refresh?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.MCPService{}, &types.MCPToolApproval{}))
	mcpRepo := repository.NewMCPServiceRepository(db)
	approvalSvc := NewMCPToolApprovalService(
		repository.NewMCPToolApprovalRepository(db), mcpRepo,
	)
	svc := NewMCPHubService(hubClient, mcpRepo, approvalSvc)

	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "user-1")
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "user-1"})
	ctx = types.WithMCPHubAccess(ctx, func(context.Context) (string, error) {
		return "delegated-token", nil
	})

	status, err := svc.Refresh(ctx, 7)
	require.NoError(t, err)
	require.True(t, status.Available)
	services, err := mcpRepo.List(ctx, 7)
	require.NoError(t, err)
	require.Len(t, services, 1)
	service := services[0]
	require.Equal(t, types.MCPServiceSourceMCPHub, service.Source)
	require.False(t, service.Enabled, "new services require administrator opt-in")
	require.True(t, mustToolEnabled(t, approvalSvc, ctx, service.ID, "query"))

	service.Enabled = true
	service.UsageInstructions = "Use for search"
	service.UpdatedAt = service.UpdatedAt.AddDate(0, 0, 1)
	require.NoError(t, mcpRepo.Update(ctx, service))
	remote[0]["tools"] = []any{
		map[string]any{"name": "query", "inputSchema": map[string]any{"type": "object"}},
		map[string]any{"name": "new_tool", "inputSchema": map[string]any{"type": "object"}},
	}
	_, err = svc.Refresh(ctx, 7)
	require.NoError(t, err)
	stored, err := mcpRepo.GetByID(ctx, 7, service.ID)
	require.NoError(t, err)
	require.True(t, stored.Enabled, "discovery must preserve administrator enablement")
	require.Equal(t, "Use for search", stored.UsageInstructions)
	require.WithinDuration(t, service.UpdatedAt, stored.UpdatedAt, 0,
		"tool-only refreshes must not invalidate the cached MCP connection")
	require.True(t, mustToolEnabled(t, approvalSvc, ctx, service.ID, "query"))
	require.False(t, mustToolEnabled(t, approvalSvc, ctx, service.ID, "new_tool"))

	remote = nil
	_, err = svc.Refresh(ctx, 7)
	require.NoError(t, err)
	stored, err = mcpRepo.GetByID(ctx, 7, service.ID)
	require.NoError(t, err)
	require.False(t, stored.HubAvailable, "removed services stay stored but unavailable")
}

func TestMCPHubFilterFailsClosedWithoutIdentity(t *testing.T) {
	hubClient, err := mcphub.NewClientWithBaseURL("http://127.0.0.1:1")
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open("file:mcp-hub-filter?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.MCPService{}, &types.MCPToolApproval{}))
	mcpRepo := repository.NewMCPServiceRepository(db)
	approvalSvc := NewMCPToolApprovalService(
		repository.NewMCPToolApprovalRepository(db), mcpRepo,
	)
	svc := NewMCPHubService(hubClient, mcpRepo, approvalSvc)
	services := []*types.MCPService{
		{ID: "manual", Source: types.MCPServiceSourceManual},
		{ID: "hub", Source: types.MCPServiceSourceMCPHub, ExternalID: "search"},
	}
	filtered, status := svc.FilterAccessible(context.Background(), 7, services)
	require.Len(t, filtered, 1)
	require.Equal(t, "manual", filtered[0].ID)
	require.False(t, status.Authenticated)
}

func mustToolEnabled(
	t *testing.T,
	approvals interface {
		IsEnabled(context.Context, uint64, string, string) (bool, error)
	},
	ctx context.Context,
	serviceID, toolName string,
) bool {
	t.Helper()
	enabled, err := approvals.IsEnabled(ctx, 7, serviceID, toolName)
	require.NoError(t, err)
	return enabled
}
