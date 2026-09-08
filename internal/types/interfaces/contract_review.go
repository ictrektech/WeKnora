package interfaces

import (
	"context"
	"io"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

type ContractReviewRepository interface {
	Create(context.Context, *types.ContractReview) error
	List(context.Context, uint64, string, bool) ([]*types.ContractReview, error)
	Get(context.Context, uint64, string, string) (*types.ContractReview, error)
	Update(context.Context, *types.ContractReview) error
	UpdateForRun(context.Context, *types.ContractReview, string) error
	Delete(context.Context, uint64, string, string) error
	ReplaceClauses(context.Context, string, []*types.ContractReviewClause) error
	ReplaceClausesForRun(context.Context, string, string, []*types.ContractReviewClause) error
	UpdateClause(context.Context, *types.ContractReviewClause) error
	UpdateClauseForRun(context.Context, *types.ContractReviewClause, string) error
	UpsertIssue(context.Context, *types.ContractReviewIssue) error
	UpsertIssueForRun(context.Context, *types.ContractReviewIssue, string) error
	ClearResults(context.Context, string) error
	ClearResultsForRun(context.Context, string, string) error
	// DeleteTenantData hard-deletes all contract review rows for a tenant,
	// including child rows and contract-review resource bindings. It returns
	// source references for post-commit physical storage cleanup.
	DeleteTenantData(context.Context, uint64) ([]types.ContractReviewResource, error)
}

type ContractReviewService interface {
	Create(context.Context, uint64, string) (*types.ContractReview, error)
	List(context.Context, uint64, string, bool) ([]*types.ContractReview, error)
	Get(context.Context, uint64, string, string) (*types.ContractReview, error)
	Update(context.Context, uint64, string, string, string, string, string, *string, *bool) (*types.ContractReview, error)
	Delete(context.Context, uint64, string, string) error
	BulkAction(context.Context, uint64, string, []string, types.ContractReviewBulkAction) (*types.ContractReviewBulkResult, error)
	Upload(context.Context, uint64, string, string, string, string, int64, io.Reader) (*types.ContractReview, error)
	OpenDocument(context.Context, uint64, string, string) (*types.ContractReview, io.ReadCloser, error)
	Start(context.Context, uint64, string, string) (*types.ContractReview, error)
	Retry(context.Context, uint64, string, string) (*types.ContractReview, error)
	Cancel(context.Context, uint64, string, string) (*types.ContractReview, error)
	// DeleteTenantData is an Owner-gated, tenant-wide purge. Database rows are
	// removed transactionally; storage cleanup errors are returned after the
	// commit. Repeating the operation is database-idempotent, while any failed
	// provider cleanup is surfaced for separate retry/operations handling.
	DeleteTenantData(context.Context, uint64) error
	Playbooks() []types.ContractReviewPlaybook
	ProcessDocument(context.Context, *asynq.Task) error
	ProcessReview(context.Context, *asynq.Task) error
}
