package types

import (
	"errors"
	"time"
)

var ErrPlatformIdentityConflict = errors.New("platform identity is already bound to another account")

type PlatformIdentity struct {
	PlatformID string    `json:"-" gorm:"primaryKey;size:255"`
	Subject    string    `json:"-" gorm:"primaryKey;size:255"`
	UserID     string    `json:"-" gorm:"size:128;not null"`
	CreatedAt  time.Time `json:"-"`
}

func (PlatformIdentity) TableName() string { return "platform_user_identities" }
