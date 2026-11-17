package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/stretchr/testify/require"
)

func TestVNoteInactiveDelegationCanOnlyDisableItsExistingConnection(t *testing.T) {
	svc, db, _, ctx, snapshot := vnoteFixture(t)
	require.NoError(t, db.AutoMigrate(&repository.VNoteDelegation{}))
	t.Setenv("SYSTEM_AES_KEY", strings.Repeat("k", 32))
	secret := strings.Repeat("a", 64)
	require.NoError(t, svc.ApplyDelegatedControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 2, true, []string{snapshot.RootID}, secret, true))
	require.Error(t, svc.ApplyDelegatedControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 3, true, []string{snapshot.RootID}, secret, false))
	require.Error(t, svc.ApplyDelegatedControl(ctx, snapshot.Subject, "different-space", 3, false, nil, secret, false))
	require.Error(t, svc.ApplyDelegatedControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 3, false, nil, strings.Repeat("b", 64), false))
	require.NoError(t, svc.ApplyDelegatedControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 3, false, nil, secret, false))
	require.Error(t, svc.ApplyDelegatedControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 2, true, []string{snapshot.RootID}, strings.Repeat("b", 64), true))
	var control repository.VNoteControl
	require.NoError(t, db.First(&control).Error)
	require.False(t, control.Enabled)
	require.Equal(t, int64(3), control.Version)
	binding, err := svc.VNoteDelegationBinding(context.Background(), secret)
	require.NoError(t, err)
	require.Equal(t, snapshot.Subject, binding.Subject)
}
