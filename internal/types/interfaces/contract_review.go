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
	Delete(context.Context, uint64, string, string) error
	ReplaceClauses(context.Context, string, []*types.ContractReviewClause) error
	UpdateClause(context.Context, *types.ContractReviewClause) error
	UpsertIssue(context.Context, *types.ContractReviewIssue) error
	ClearResults(context.Context, string) error
}

type ContractReviewService interface {
	Create(context.Context, uint64, string) (*types.ContractReview, error)
	List(context.Context, uint64, string, bool) ([]*types.ContractReview, error)
	Get(context.Context, uint64, string, string) (*types.ContractReview, error)
	Update(context.Context, uint64, string, string, string, string, string, *bool) (*types.ContractReview, error)
	Delete(context.Context, uint64, string, string) error
	BulkAction(context.Context, uint64, string, []string, types.ContractReviewBulkAction) (*types.ContractReviewBulkResult, error)
	Upload(context.Context, uint64, string, string, string, string, int64, io.Reader) (*types.ContractReview, error)
	OpenDocument(context.Context, uint64, string, string) (*types.ContractReview, io.ReadCloser, error)
	Start(context.Context, uint64, string, string) (*types.ContractReview, error)
	Retry(context.Context, uint64, string, string) (*types.ContractReview, error)
	Playbooks() []types.ContractReviewPlaybook
	ProcessDocument(context.Context, *asynq.Task) error
	ProcessReview(context.Context, *asynq.Task) error
}
