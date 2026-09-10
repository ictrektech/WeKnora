package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// DocumentParseArtifactRepository stores normalized parser/OCR output so
// multiple downstream pipelines can consume one deterministic parse result.
type DocumentParseArtifactRepository interface {
	GetByID(context.Context, uint64, string) (*types.DocumentParseArtifact, error)
	GetByFingerprint(context.Context, uint64, string, string) (*types.DocumentParseArtifact, error)
	Upsert(context.Context, *types.DocumentParseArtifact) error
	DeleteBySourceDocument(context.Context, uint64, string) error
}
