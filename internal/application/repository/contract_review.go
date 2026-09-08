package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type contractReviewRepository struct{ db *gorm.DB }

func lockContractReview(tx *gorm.DB, reviewID string) error {
	var review types.ContractReview
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", reviewID).First(&review).Error
}

func lockContractReviewRun(tx *gorm.DB, reviewID, analysisRunID string) error {
	var review types.ContractReview
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
		Where("id = ? AND analysis_run_id = ?", reviewID, analysisRunID).First(&review).Error
}

func NewContractReviewRepository(db *gorm.DB) interfaces.ContractReviewRepository {
	return &contractReviewRepository{db: db}
}

func (r *contractReviewRepository) Create(ctx context.Context, review *types.ContractReview) error {
	return r.db.WithContext(ctx).Create(review).Error
}

func (r *contractReviewRepository) List(ctx context.Context, tenantID uint64, userID string, archived bool) ([]*types.ContractReview, error) {
	var rows []*types.ContractReview
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenantID, userID)
	if archived {
		q = q.Where("archived_at IS NOT NULL")
	} else {
		q = q.Where("archived_at IS NULL")
	}
	err := q.Order("updated_at DESC").Find(&rows).Error
	return rows, err
}

func (r *contractReviewRepository) Get(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	var review types.ContractReview
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND id = ?", tenantID, userID, id).
		Preload("Clauses", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC") }).
		Preload("Issues", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC") }).First(&review).Error
	return &review, err
}

func (r *contractReviewRepository) Update(ctx context.Context, review *types.ContractReview) error {
	result := r.db.WithContext(ctx).Model(&types.ContractReview{}).
		Where("tenant_id = ? AND user_id = ? AND id = ?", review.TenantID, review.UserID, review.ID).
		Select("*").Omit("id", "created_at", "deleted_at", "Clauses", "Issues").Updates(review)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *contractReviewRepository) UpdateForRun(ctx context.Context, review *types.ContractReview, analysisRunID string) error {
	result := r.db.WithContext(ctx).Model(&types.ContractReview{}).
		Where("tenant_id = ? AND user_id = ? AND id = ? AND analysis_run_id = ?", review.TenantID, review.UserID, review.ID, analysisRunID).
		Select("*").Omit("id", "created_at", "deleted_at", "Clauses", "Issues").Updates(review)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *contractReviewRepository) Delete(ctx context.Context, tenantID uint64, userID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var review types.ContractReview
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id = ? AND user_id = ? AND id = ?", tenantID, userID, id).First(&review).Error; err != nil {
			return err
		}
		if err := tx.Where("review_id = ?", id).Delete(&types.ContractReviewIssue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("review_id = ?", id).Delete(&types.ContractReviewClause{}).Error; err != nil {
			return err
		}
		return tx.Where("tenant_id = ? AND user_id = ? AND id = ?", tenantID, userID, id).Delete(&types.ContractReview{}).Error
	})
}

// DeleteTenantData hard-deletes a tenant's contract-review aggregates and all
// dependent rows in one transaction. Resource bindings are removed here too;
// the returned references are cleaned from physical storage by the service
// after this transaction commits. This keeps a retry safe when the storage
// provider is temporarily unavailable.
func (r *contractReviewRepository) DeleteTenantData(ctx context.Context, tenantID uint64) ([]types.ContractReviewResource, error) {
	var resources []types.ContractReviewResource
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var reviews []types.ContractReview
		if err := tx.Unscoped().Select("id", "resource_ref").Where("tenant_id = ?", tenantID).Find(&reviews).Error; err != nil {
			return err
		}
		if len(reviews) == 0 {
			return nil
		}

		reviewIDs := make([]string, 0, len(reviews))
		for _, review := range reviews {
			reviewIDs = append(reviewIDs, review.ID)
			if review.ResourceRef != "" {
				resources = append(resources, types.ContractReviewResource{
					ReviewID:  review.ID,
					Reference: review.ResourceRef,
				})
			}
		}
		if err := tx.Unscoped().Where("review_id IN ?", reviewIDs).Delete(&types.ContractReviewIssue{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("review_id IN ?", reviewIDs).Delete(&types.ContractReviewClause{}).Error; err != nil {
			return err
		}
		if err := tx.Where("owner_type = ? AND owner_id IN ?", types.ResourceOwnerContractReview, reviewIDs).
			Delete(&types.ResourceBinding{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("tenant_id = ? AND id IN ?", tenantID, reviewIDs).
			Delete(&types.ContractReview{}).Error
	})
	if err != nil {
		return nil, err
	}
	return resources, nil
}

func (r *contractReviewRepository) ReplaceClauses(ctx context.Context, reviewID string, rows []*types.ContractReviewClause) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReview(tx, reviewID); err != nil {
			return err
		}
		if err := tx.Where("review_id = ?", reviewID).Delete(&types.ContractReviewClause{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

func (r *contractReviewRepository) ReplaceClausesForRun(ctx context.Context, reviewID, analysisRunID string, rows []*types.ContractReviewClause) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReviewRun(tx, reviewID, analysisRunID); err != nil {
			return err
		}
		if err := tx.Where("review_id = ?", reviewID).Delete(&types.ContractReviewClause{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

func (r *contractReviewRepository) UpdateClause(ctx context.Context, row *types.ContractReviewClause) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReview(tx, row.ReviewID); err != nil {
			return err
		}
		return tx.Save(row).Error
	})
}

func (r *contractReviewRepository) UpdateClauseForRun(ctx context.Context, row *types.ContractReviewClause, analysisRunID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReviewRun(tx, row.ReviewID, analysisRunID); err != nil {
			return err
		}
		return tx.Save(row).Error
	})
}

func (r *contractReviewRepository) UpsertIssue(ctx context.Context, issue *types.ContractReviewIssue) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReview(tx, issue.ReviewID); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "fingerprint"}}, DoNothing: true,
		}).Create(issue).Error
	})
}

func (r *contractReviewRepository) UpsertIssueForRun(ctx context.Context, issue *types.ContractReviewIssue, analysisRunID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReviewRun(tx, issue.ReviewID, analysisRunID); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "fingerprint"}}, DoNothing: true,
		}).Create(issue).Error
	})
}

func (r *contractReviewRepository) ClearResults(ctx context.Context, reviewID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReview(tx, reviewID); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("review_id = ?", reviewID).Delete(&types.ContractReviewIssue{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("review_id = ?", reviewID).Delete(&types.ContractReviewClause{}).Error
	})
}

func (r *contractReviewRepository) ClearResultsForRun(ctx context.Context, reviewID, analysisRunID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContractReviewRun(tx, reviewID, analysisRunID); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("review_id = ?", reviewID).Delete(&types.ContractReviewIssue{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("review_id = ?", reviewID).Delete(&types.ContractReviewClause{}).Error
	})
}
