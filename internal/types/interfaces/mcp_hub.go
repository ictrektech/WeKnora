package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// MCPHubClient lists the MCP services visible to a VOS user.
type MCPHubClient interface {
	ListServers(ctx context.Context, accessToken string) ([]*types.MCPHubServer, error)
}

// MCPHubService reconciles MCPHub services into the workspace and enforces
// their per-user discovery boundary.
type MCPHubService interface {
	Refresh(ctx context.Context, tenantID uint64) (*types.MCPHubDiscoveryStatus, error)
	FilterAccessible(
		ctx context.Context,
		tenantID uint64,
		services []*types.MCPService,
	) ([]*types.MCPService, *types.MCPHubDiscoveryStatus)
	EnsureToolPolicies(
		ctx context.Context,
		tenantID uint64,
		serviceID string,
		tools []*types.MCPTool,
		defaultEnabled bool,
	) error
}
