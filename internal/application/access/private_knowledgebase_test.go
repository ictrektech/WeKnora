package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPrivateKBRequiresOwnerInsteadOfWorkspaceOrShare(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "private", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}
	for _, example := range []struct {
		name, user string
		tenant     uint64
		principal  string
		allowed    bool
	}{
		{"owner", "alice", 7, types.PrincipalWebUser, true},
		{"workspace_member", "bob", 7, types.PrincipalWebUser, false},
		{"workspace_admin", "admin", 7, types.PrincipalWebUser, false},
		{"other_workspace", "alice", 8, types.PrincipalWebUser, false},
		{"embed", "alice", 7, types.PrincipalEmbedSession, false},
		{"API", "alice", 7, types.PrincipalAPITenant, false},
	} {
		t.Run(example.name, func(t *testing.T) {
			caller := types.Caller{TenantID: example.tenant, UserID: example.user, Role: types.TenantRoleOwner}
			ctx := types.WithPrincipal(types.WithCaller(context.Background(), caller), types.Principal{Type: example.principal, ID: example.user})
			grant, err := ResolveKB(ctx, KBRequest{Caller: caller}, kb, types.OrgRoleViewer, &shareLookup{permission: types.OrgRoleAdmin}, &agentLookup{any: true})
			if example.allowed {
				require.NoError(t, err)
				require.NotNil(t, grant)
			} else {
				require.ErrorIs(t, err, ErrNotFound)
				require.Nil(t, grant)
			}
			if !example.allowed {
				require.Error(t, AuthorizeKBAccess(ctx, nil, []*types.KnowledgeBase{kb}))
			}
		})
	}
	owner := types.WithCaller(context.Background(), types.Caller{UserID: "alice", TenantID: 7, Role: types.TenantRoleOwner})
	shared := WithSharedAgent(owner, &types.CustomAgent{ID: "shared", TenantID: 7})
	require.False(t, CanReadPrivateKB(shared, kb))
	automation := types.WithPrincipal(owner, types.Principal{Type: types.PrincipalWebUser, ID: "alice", ActingVia: "automation:job"})
	require.False(t, CanReadPrivateKB(automation, kb), "unattended actors cannot inherit an owner's private-copy read permission")
	checks := 0
	live := types.WithVNoteReadCheck(owner, func(id string) bool { checks++; return false })
	require.False(t, CanReadPrivateKB(live, kb))
	require.Equal(t, 1, checks)
	require.True(t, CanReadPrivateKB(live, &types.KnowledgeBase{ID: "ordinary", TenantID: 7}))
	require.Equal(t, 1, checks)
	require.True(t, CanLoadPrivateKB(context.Background(), kb), "trusted worker loading does not itself grant read access")
	require.False(t, CanReadPrivateKB(context.Background(), kb))
	ordinary := &types.KnowledgeBase{ID: "ordinary", TenantID: 7}
	require.True(t, CanReadPrivateKB(shared, ordinary))
	_, err := ResolveKB(owner, KBRequest{Caller: types.CallerFromContext(owner)}, kb, types.OrgRoleEditor, nil, nil)
	require.ErrorIs(t, err, ErrForbidden, "generic editor grants must not bypass the source integration")
	manual := (&KBAccess{KnowledgeBase: kb, Caller: types.CallerFromContext(owner), EffectiveTenantID: 7, Permission: types.OrgRoleEditor}).Context(owner)
	require.ErrorIs(t, RequireKBWrite(manual, kb), ErrForbidden)
	admitted, err := WithKBTaskWrite(owner, kb, 7)
	require.NoError(t, err)
	require.NoError(t, RequireKBWrite(admitted, kb))
}

type privacyShareLookup struct {
	shareLookup
	calls   int
	visible bool
}

func (lookup *privacyShareLookup) CanReadPrivateKnowledgeBase(context.Context, string) (bool, error) {
	lookup.calls++
	return lookup.visible, nil
}

func TestPrivateVisibilityPrecedesSameTenantAndCachedGrants(t *testing.T) {
	ctx := types.WithCaller(context.Background(), types.Caller{UserID: "bob", TenantID: 7, Role: types.TenantRoleOwner})
	lookup := &privacyShareLookup{visible: false}
	permissions := NewKBPermissions(ctx, lookup)
	for range 5 {
		allowed, err := permissions.Check("private", 7, types.OrgRoleViewer)
		require.NoError(t, err)
		require.False(t, allowed)
	}
	require.Equal(t, 1, lookup.calls)
	ctx = types.WithCaller(context.Background(), types.Caller{TenantID: 7})
	permissions = NewKBPermissions(ctx, lookup).WithoutOrganizationShares()
	allowed, err := permissions.Check("private", 7, types.OrgRoleViewer)
	require.NoError(t, err)
	require.False(t, allowed)
}
