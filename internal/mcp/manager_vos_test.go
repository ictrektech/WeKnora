package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestResolveVOSDelegatedServiceUsesRequestUserToken(t *testing.T) {
	service := &types.MCPService{
		ID:     "hub-service",
		Source: types.MCPServiceSourceMCPHub,
		AuthConfig: &types.MCPAuthConfig{
			AuthType: types.MCPAuthVOS,
		},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
	ctx = types.WithMCPHubAccess(ctx, func(context.Context) (string, error) {
		return "vos-user-token", nil
	})

	resolved, key, err := resolveVOSDelegatedService(ctx, service)
	require.NoError(t, err)
	require.NotSame(t, service, resolved)
	require.Equal(t, types.MCPAuthBearer, resolved.AuthConfig.AuthType)
	require.Equal(t, "vos-user-token", resolved.AuthConfig.Token)
	require.Contains(t, key, "web_user:alice")
	require.NotContains(t, key, "vos-user-token")
	require.Equal(t, types.MCPAuthVOS, service.AuthConfig.AuthType, "stored config stays delegated")
}

func TestResolveVOSDelegatedServiceRequiresFreshProof(t *testing.T) {
	service := &types.MCPService{
		ID:     "hub-service",
		Source: types.MCPServiceSourceMCPHub,
		AuthConfig: &types.MCPAuthConfig{
			AuthType: types.MCPAuthVOS,
		},
	}
	ctx := types.WithPrincipal(context.Background(), types.Principal{
		Type: types.PrincipalWebUser, ID: "alice",
	})
	_, _, err := resolveVOSDelegatedService(ctx, service)
	require.True(t, errors.Is(err, types.ErrMCPHubSignInRequired))
}
