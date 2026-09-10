package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// smartArchiveMutationKey marks the narrow internal path used to maintain the
// system-owned archive mirror. It is deliberately context-scoped; callers
// still need a tenant-scoped archive service and the managed KB marker.
type smartArchiveMutationKey struct{}

func withSmartArchiveMutation(ctx context.Context) context.Context {
	return context.WithValue(ctx, smartArchiveMutationKey{}, true)
}

func isSmartArchiveMutation(ctx context.Context) bool {
	value, _ := ctx.Value(smartArchiveMutationKey{}).(bool)
	return value
}

func isManagedSmartArchiveKnowledgeBase(kb *types.KnowledgeBase) bool {
	if kb == nil {
		return false
	}
	description := strings.TrimSpace(kb.Description)
	return strings.HasPrefix(description, types.ManagedSmartArchiveKnowledgeBaseMarker) ||
		strings.HasPrefix(description, types.LegacyManagedSmartArchiveKnowledgeBaseMarker)
}

func rejectManagedSmartArchiveMutation(ctx context.Context, kb *types.KnowledgeBase) error {
	if isManagedSmartArchiveKnowledgeBase(kb) && !isSmartArchiveMutation(ctx) {
		return ErrManagedSmartArchiveKnowledgeBase
	}
	return nil
}
