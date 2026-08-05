package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type userModelDesensitizationRepository struct {
	db *gorm.DB
}

func NewUserModelDesensitizationRepository(db *gorm.DB) interfaces.UserModelDesensitizationRepository {
	return &userModelDesensitizationRepository{db: db}
}

func (r *userModelDesensitizationRepository) Get(
	ctx context.Context, userID, modelID string,
) (*types.UserModelDesensitization, error) {
	var preference types.UserModelDesensitization
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND model_id = ?", userID, modelID).
		First(&preference).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &types.UserModelDesensitization{UserID: userID, ModelID: modelID}, nil
	}
	return &preference, err
}

func (r *userModelDesensitizationRepository) Upsert(
	ctx context.Context, preference *types.UserModelDesensitization,
) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "model_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"enabled":    preference.Enabled,
			"ner":        preference.NER,
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		}),
	}).Create(preference).Error
}
