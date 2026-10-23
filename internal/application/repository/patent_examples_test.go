package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPatentExamplesPreferencePersistsAndIsolatesTenants(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewTenantRepository(db)
	hidden := false
	first := &types.Tenant{Name: "hidden examples", PatentWorkspaceConfig: &types.PatentWorkspaceConfig{Enabled: true, ExamplesEnabled: &hidden}}
	second := &types.Tenant{Name: "legacy display preference", PatentWorkspaceConfig: &types.PatentWorkspaceConfig{Enabled: true}}
	require.NoError(t, repo.CreateTenant(ctx, first))
	require.NoError(t, repo.CreateTenant(ctx, second))
	// New repository instances simulate subsequent requests/processes, not a browser cache.
	for _, enabled := range []bool{false, true} {
		reloaded, err := NewTenantRepository(db).GetTenantByID(ctx, first.ID)
		require.NoError(t, err)
		require.False(t, reloaded.PatentWorkspaceConfig.AreExamplesEnabled())
		reloaded.PatentWorkspaceConfig.Enabled = enabled
		require.NoError(t, repo.UpdateTenant(ctx, reloaded))
		untouched, err := NewTenantRepository(db).GetTenantByID(ctx, second.ID)
		require.NoError(t, err)
		require.True(t, untouched.PatentWorkspaceConfig.IsEnabled())
		require.True(t, untouched.PatentWorkspaceConfig.AreExamplesEnabled())
	}
	reloaded, err := NewTenantRepository(db).GetTenantByID(ctx, first.ID)
	require.NoError(t, err)
	require.True(t, reloaded.PatentWorkspaceConfig.IsEnabled())
	require.False(t, reloaded.PatentWorkspaceConfig.AreExamplesEnabled())
}
