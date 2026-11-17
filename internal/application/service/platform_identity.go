package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

type platformIdentityStore interface {
	GetPlatformIdentityUser(context.Context, string, string) (*types.User, error)
	BindPlatformIdentity(context.Context, string, string, string) error
	GetPlatformSubjectForUser(context.Context, string, string) (string, error)
}

type platformIdentityUserCreator interface {
	CreateUserWithPlatformIdentity(context.Context, *types.User, string, string) error
}

func (s *userService) GetPlatformIdentityUser(ctx context.Context, platformID, subject string) (*types.User, error) {
	store, supported := s.userRepo.(platformIdentityStore)
	if !supported {
		return nil, errors.New("platform identity storage is unavailable")
	}
	return store.GetPlatformIdentityUser(ctx, platformID, subject)
}

func (s *userService) BindPlatformIdentity(ctx context.Context, platformID, subject, userID string) error {
	store, supported := s.userRepo.(platformIdentityStore)
	if !supported {
		return errors.New("platform identity storage is unavailable")
	}
	return store.BindPlatformIdentity(ctx, platformID, subject, userID)
}
