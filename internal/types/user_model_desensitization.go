package types

import "time"

// UserModelDesensitization stores one user's redaction policy for one exact
// model record. Model name, provider and endpoint are intentionally absent:
// two model records that happen to share those values must remain independent.
type UserModelDesensitization struct {
	UserID    string    `json:"-" gorm:"type:varchar(36);primaryKey"`
	ModelID   string    `json:"model_id" gorm:"type:varchar(64);primaryKey"`
	Enabled   bool      `json:"enabled" gorm:"not null;default:false"`
	NER       bool      `json:"ner" gorm:"not null;default:false"`
	BaseURL   string    `json:"base_url" gorm:"type:varchar(2048);not null;default:''"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserModelDesensitization) TableName() string {
	return "user_model_desensitizations"
}
