package repository

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPlatformIdentityCreationIsAtomic(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := database.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.AutoMigrate(&types.User{}))
	migration, err := os.ReadFile("../../../migrations/sqlite/000064_platform_user_identities.up.sql")
	require.NoError(t, err)
	require.NoError(t, database.Exec(string(migration)).Error)
	repository := &userRepository{db: database}
	ctx := context.Background()
	alice := &types.User{ID: "alice", Email: "alice@local", Username: "alice", IsActive: true}
	require.NoError(t, repository.CreateUserWithPlatformIdentity(ctx, alice, "vos-a", "subject-a"))
	mapped, err := repository.GetPlatformIdentityUser(ctx, "vos-a", "subject-a")
	require.NoError(t, err)
	require.Equal(t, alice.ID, mapped.ID)
	bob := &types.User{ID: "bob", Email: "bob@local", Username: "bob", IsActive: true}
	require.ErrorIs(t, repository.CreateUserWithPlatformIdentity(ctx, bob, "vos-a", "subject-a"), types.ErrPlatformIdentityConflict)
	_, err = repository.GetUserByID(ctx, bob.ID)
	require.ErrorIs(t, err, ErrUserNotFound)
	var count int64
	require.NoError(t, database.Model(&types.PlatformIdentity{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestPlatformIdentityBindingIsolationAndLifecycle(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := database.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Exec(`PRAGMA foreign_keys = ON`).Error)
	require.NoError(t, database.Exec(`CREATE TABLE users (id VARCHAR(128) PRIMARY KEY, email TEXT, username TEXT, is_active BOOLEAN NOT NULL, deleted_at DATETIME)`).Error)
	require.NoError(t, database.Exec(`INSERT INTO users (id,email,username,is_active) VALUES ('alice','alice@local','alice',1),('bob','bob@local','bob',1),('disabled','disabled@local','disabled',0)`).Error)
	migration, err := os.ReadFile("../../../migrations/sqlite/000064_platform_user_identities.up.sql")
	require.NoError(t, err)
	require.NoError(t, database.Exec(string(migration)).Error)
	repository := &userRepository{db: database}
	ctx := context.Background()
	require.NoError(t, repository.BindPlatformIdentity(ctx, "vos-a", "subject-a", "alice"))
	require.NoError(t, repository.BindPlatformIdentity(ctx, "vos-a", "subject-a", "alice"))
	require.ErrorIs(t, repository.BindPlatformIdentity(ctx, "vos-a", "subject-a", "bob"), types.ErrPlatformIdentityConflict)
	require.ErrorIs(t, repository.BindPlatformIdentity(ctx, "vos-a", "subject-b", "alice"), types.ErrPlatformIdentityConflict)
	require.ErrorIs(t, repository.BindPlatformIdentity(ctx, "vos-a", "subject-disabled", "disabled"), ErrUserNotFound)
	require.NoError(t, repository.BindPlatformIdentity(ctx, "vos-b", "subject-a", "bob"))
	require.NoError(t, database.Exec(`UPDATE users SET username='renamed', email='renamed@local' WHERE id='alice'`).Error)
	user, err := repository.GetPlatformIdentityUser(ctx, "vos-a", "subject-a")
	require.NoError(t, err)
	require.Equal(t, "alice", user.ID)
	require.Equal(t, "renamed", user.Username)
	_, err = repository.GetPlatformIdentityUser(ctx, "other-vos", "subject-a")
	require.ErrorIs(t, err, ErrUserNotFound)
	require.NoError(t, database.Exec(`DELETE FROM users WHERE id='alice'`).Error)
	var count int64
	require.NoError(t, database.Model(&types.PlatformIdentity{}).Where("platform_id='vos-a'").Count(&count).Error)
	require.Zero(t, count)

	var wait sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- repository.BindPlatformIdentity(ctx, "vos-b", "subject-a", "bob")
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
	var total int64
	require.NoError(t, database.Model(&types.PlatformIdentity{}).Count(&total).Error)
	require.EqualValues(t, 1, total)
}
