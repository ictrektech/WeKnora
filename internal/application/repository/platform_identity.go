package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *userRepository) GetPlatformIdentityUser(ctx context.Context, platformID, subject string) (*types.User, error) {
	var identity types.PlatformIdentity
	err := r.db.WithContext(ctx).Where("platform_id = ? AND subject = ?", platformID, subject).First(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.GetUserByID(ctx, identity.UserID)
}

// CreateUserWithPlatformIdentity binds only an account created in this transaction.
// An existing account is never selected by its display name or email here.
func (r *userRepository) CreateUserWithPlatformIdentity(ctx context.Context, user *types.User, platformID, subject string) error {
	return r.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		scoped := &userRepository{db: transaction}
		if err := scoped.CreateUser(ctx, user); err != nil {
			return err
		}
		return scoped.BindPlatformIdentity(ctx, platformID, subject, user.ID)
	})
}

func (r *userRepository) BindPlatformIdentity(ctx context.Context, platformID, subject, userID string) error {
	if strings.TrimSpace(platformID) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(userID) == "" ||
		len(platformID) > 255 || len(subject) > 255 || len(userID) > 128 {
		return errors.New("invalid platform identity")
	}
	return r.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var user types.User
		if err := transaction.Where("id = ? AND is_active = ?", userID, true).First(&user).Error; err != nil {
			return ErrUserNotFound
		}
		identity := types.PlatformIdentity{PlatformID: platformID, Subject: subject, UserID: userID, CreatedAt: time.Now().UTC()}
		if err := transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&identity).Error; err != nil {
			return fmt.Errorf("bind platform identity: %w", err)
		}
		var stored types.PlatformIdentity
		if err := transaction.Where("platform_id = ? AND subject = ?", platformID, subject).First(&stored).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return types.ErrPlatformIdentityConflict
			}
			return err
		}
		if stored.UserID != userID {
			return types.ErrPlatformIdentityConflict
		}
		return nil
	})
}

func (r *userRepository) GetPlatformSubjectForUser(ctx context.Context, platformID, userID string) (string, error) {
	var identity types.PlatformIdentity
	err := r.db.WithContext(ctx).Where("platform_id = ? AND user_id = ?", platformID, userID).First(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return identity.Subject, err
}
