package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type UserModelDesensitizationRepository interface {
	Get(ctx context.Context, userID, modelID string) (*types.UserModelDesensitization, error)
	Upsert(ctx context.Context, preference *types.UserModelDesensitization) error
}
