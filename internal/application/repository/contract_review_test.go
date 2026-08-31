package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type contractReviewRepositoryFixture struct {
	repo interfaces.ContractReviewRepository
	db   *gorm.DB
}

func newContractReviewRepositoryFixture(t *testing.T) contractReviewRepositoryFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.ContractReview{}, &types.ContractReviewClause{}, &types.ContractReviewIssue{}))
	return contractReviewRepositoryFixture{repo: NewContractReviewRepository(db), db: db}
}

func TestContractReviewRepositoryScopesListsAndGetsByOwner(t *testing.T) {
	fixture := newContractReviewRepositoryFixture(t)
	ctx := context.Background()
	one := &types.ContractReview{TenantID: 1, UserID: "u1", Title: "Mine"}
	two := &types.ContractReview{TenantID: 1, UserID: "u2", Title: "Theirs"}
	require.NoError(t, fixture.repo.Create(ctx, one))
	require.NoError(t, fixture.repo.Create(ctx, two))
	rows, err := fixture.repo.List(ctx, 1, "u1", false)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, one.ID, rows[0].ID)
	_, err = fixture.repo.Get(ctx, 1, "u2", one.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestContractReviewRepositoryArchiveAndIdempotentIssue(t *testing.T) {
	fixture := newContractReviewRepositoryFixture(t)
	ctx := context.Background()
	review := &types.ContractReview{TenantID: 1, UserID: "u1"}
	require.NoError(t, fixture.repo.Create(ctx, review))
	clause := &types.ContractReviewClause{ReviewID: review.ID, Sequence: 0, Title: "Payment"}
	require.NoError(t, fixture.repo.ReplaceClauses(ctx, review.ID, []*types.ContractReviewClause{clause}))
	issue := &types.ContractReviewIssue{ReviewID: review.ID, ClauseID: clause.ID, Fingerprint: "same", RiskLevel: types.ContractReviewRiskHigh, Title: "Early payment", Explanation: "risk", OriginalQuote: "pay", Suggestion: "revise"}
	require.NoError(t, fixture.repo.UpsertIssue(ctx, issue))
	duplicate := *issue
	duplicate.ID = "another-id"
	require.NoError(t, fixture.repo.UpsertIssue(ctx, &duplicate))
	loaded, err := fixture.repo.Get(ctx, 1, "u1", review.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Issues, 1)
	now := time.Now()
	review.ArchivedAt = &now
	require.NoError(t, fixture.repo.Update(ctx, review))
	active, _ := fixture.repo.List(ctx, 1, "u1", false)
	archived, _ := fixture.repo.List(ctx, 1, "u1", true)
	require.Empty(t, active)
	require.Len(t, archived, 1)
}

func TestContractReviewRepositoryDeleteRemovesChildren(t *testing.T) {
	fixture := newContractReviewRepositoryFixture(t)
	ctx := context.Background()
	review := &types.ContractReview{TenantID: 1, UserID: "u1"}
	require.NoError(t, fixture.repo.Create(ctx, review))
	clause := &types.ContractReviewClause{ReviewID: review.ID, Sequence: 0, Title: "Term"}
	require.NoError(t, fixture.repo.ReplaceClauses(ctx, review.ID, []*types.ContractReviewClause{clause}))
	require.NoError(t, fixture.repo.Delete(ctx, 1, "u1", review.ID))
	var clauseCount int64
	require.NoError(t, fixture.db.Model(&types.ContractReviewClause{}).Where("review_id = ?", review.ID).Count(&clauseCount).Error)
	require.Zero(t, clauseCount)
}
