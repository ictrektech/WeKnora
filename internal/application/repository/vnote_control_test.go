package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestVNoteControlMonotonicAndIdentityBound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, db.AutoMigrate(&VNoteControl{}))
	ctx := context.Background()
	apply := func(subject string, version int64, enabled bool, roots ...string) error {
		return ApplyVNoteControl(ctx, db, "user", "personal", subject, version, enabled, roots)
	}
	require.NoError(t, apply("subject", 1, true, "b", "a"))
	allowed, err := VNoteControlAllows(ctx, db, "user", "personal", "subject", "a", 1)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = VNoteControlAllows(ctx, db, "user", "personal", "subject", "other", 1)
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, apply("subject", 1, true, "a", "b"))
	require.ErrorIs(t, apply("subject", 1, false), ErrVNoteControlConflict)
	require.ErrorIs(t, apply("other-subject", 2, false), ErrVNoteControlConflict)
	require.NoError(t, apply("subject", 2, false, "a"))
	allowed, err = VNoteControlAllows(ctx, db, "user", "personal", "subject", "a", 1)
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = VNoteControlAllows(ctx, db, "user", "personal", "subject", "a", 2)
	require.NoError(t, err)
	require.False(t, allowed)
	require.ErrorIs(t, apply("subject", 1, true, "a", "b"), ErrVNoteControlStale)
	var stored VNoteControl
	require.NoError(t, db.First(&stored).Error)
	require.EqualValues(t, 2, stored.Version)
	require.False(t, stored.Enabled)
	require.Equal(t, `["a"]`, stored.RootsJSON)
	require.ErrorIs(t, apply("subject", 3, true, "a", "a"), ErrVNoteControlConflict)
}
