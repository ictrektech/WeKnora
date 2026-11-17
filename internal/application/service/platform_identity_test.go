package service

import (
	"context"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type identityAwareUserRepository struct {
	*stubUserRepoForAuth
	boundSubject     string
	platformID       string
	userID           string
	createBoundCalls int
}

func (repository *identityAwareUserRepository) CreateUserWithPlatformIdentity(_ context.Context, user *types.User, platformID, subject string) error {
	repository.createBoundCalls++
	repository.users[user.ID] = user
	repository.platformID, repository.boundSubject, repository.userID = platformID, subject, user.ID
	return nil
}

func TestPlatformIdentityCreationBindsOnlyNewAccounts(t *testing.T) {
	ctx := context.Background()
	repository := &identityAwareUserRepository{stubUserRepoForAuth: &stubUserRepoForAuth{users: map[string]*types.User{}}}
	service := &userService{userRepo: repository}
	request := &types.TrustedIdentityLoginRequest{Provider: "vos-oidc", PlatformID: "vos-a", BindOnCreate: true, Subject: "subject-a", Username: "alice", Email: "alice@local"}
	user, err := service.ResolveTrustedIdentityUser(ctx, request, types.TenantProvisioningTenantless)
	require.NoError(t, err)
	require.Equal(t, 1, repository.createBoundCalls)
	mapped, err := repository.GetPlatformIdentityUser(ctx, "vos-a", "subject-a")
	require.NoError(t, err)
	require.Equal(t, user.ID, mapped.ID)
	request.Username, request.Email = "renamed", "renamed@local"
	renamed, err := service.ResolveTrustedIdentityUser(ctx, request, types.TenantProvisioningTenantless)
	require.NoError(t, err)
	require.Equal(t, user.ID, renamed.ID)
	require.Equal(t, 1, repository.createBoundCalls)

	legacy := &types.User{ID: "legacy", Username: "bob", Email: "bob@local", IsActive: true}
	repository = &identityAwareUserRepository{stubUserRepoForAuth: &stubUserRepoForAuth{users: map[string]*types.User{legacy.ID: legacy}}}
	service = &userService{userRepo: repository}
	resolved, err := service.ResolveTrustedIdentityUser(ctx, &types.TrustedIdentityLoginRequest{Provider: "vos-oidc", PlatformID: "vos-a", BindOnCreate: true, Subject: "subject-b", Username: "bob", Email: "bob@local"}, types.TenantProvisioningTenantless)
	require.NoError(t, err)
	require.Equal(t, legacy.ID, resolved.ID)
	require.Zero(t, repository.createBoundCalls)
	require.Empty(t, repository.boundSubject)
}

func (repository *identityAwareUserRepository) GetPlatformIdentityUser(ctx context.Context, platformID, subject string) (*types.User, error) {
	if platformID != repository.platformID || subject != repository.boundSubject {
		return nil, apprepo.ErrUserNotFound
	}
	return repository.GetUserByID(ctx, repository.userID)
}

func (repository *identityAwareUserRepository) BindPlatformIdentity(context.Context, string, string, string) error {
	return nil
}

func (repository *identityAwareUserRepository) GetPlatformSubjectForUser(_ context.Context, platformID, userID string) (string, error) {
	if platformID == repository.platformID && userID == repository.userID {
		return repository.boundSubject, nil
	}
	return "", nil
}

func TestPlatformIdentityResolvesRenamedUserAndRejectsReusedName(t *testing.T) {
	user := &types.User{ID: "stable-local-user", Email: "old-name@local", Username: "old-name", IsActive: true}
	repository := &identityAwareUserRepository{
		stubUserRepoForAuth: &stubUserRepoForAuth{users: map[string]*types.User{user.ID: user}},
		boundSubject:        "subject-a", platformID: "vos-a", userID: user.ID,
	}
	service := &userService{userRepo: repository}
	renamed, err := service.ResolveTrustedIdentityUser(context.Background(), &types.TrustedIdentityLoginRequest{
		PlatformID: "vos-a", Provider: "vos-oidc", Subject: "subject-a", Username: "new-name", Email: "new-name@local",
	}, types.TenantProvisioningCreatePersonal)
	require.NoError(t, err)
	require.Equal(t, user.ID, renamed.ID)
	_, err = service.ResolveTrustedIdentityUser(context.Background(), &types.TrustedIdentityLoginRequest{
		PlatformID: "vos-a", Provider: "vos-oidc", Subject: "subject-b", Username: "old-name", Email: "old-name@local",
	}, types.TenantProvisioningCreatePersonal)
	require.ErrorIs(t, err, types.ErrPlatformIdentityConflict)
	user.IsActive = false
	disabled, err := service.ResolveTrustedIdentityUser(context.Background(), &types.TrustedIdentityLoginRequest{
		PlatformID: "vos-a", Provider: "vos-oidc", Subject: "subject-a", Username: "new-name", Email: "new-name@local",
	}, types.TenantProvisioningCreatePersonal)
	require.NoError(t, err)
	require.False(t, disabled.IsActive)
}
