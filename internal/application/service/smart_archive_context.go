package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// smartArchiveMutationKey marks the narrow internal path used to maintain the
// system-owned archive mirror. It is deliberately context-scoped; callers
// still need a tenant-scoped archive service and the managed KB marker.
type smartArchiveMutationKey struct{}

func withSmartArchiveMutation(ctx context.Context) context.Context {
	return context.WithValue(ctx, smartArchiveMutationKey{}, true)
}

// resolveManagedArchiveKnowledgeBase loads and validates the server-side KB
// before any internal write grant is minted. The repository path is preferred
// because it enforces the tenant in the query; the service fallback keeps the
// validation explicit for lightweight deployments and test doubles.
func (s *smartArchiveService) resolveManagedArchiveKnowledgeBase(ctx context.Context, kbID string, tenantID uint64) (*types.KnowledgeBase, error) {
	kbID = strings.TrimSpace(kbID)
	if kbID == "" || tenantID == 0 {
		return nil, errors.New("smart archive managed knowledge base context is unavailable")
	}

	var (
		kb  *types.KnowledgeBase
		err error
	)
	switch {
	case s.kbRepo != nil:
		kb, err = s.kbRepo.GetKnowledgeBaseByIDAndTenant(ctx, kbID, tenantID)
	case s.kbs != nil:
		kb, err = s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	default:
		return nil, errors.New("smart archive knowledge base service is unavailable")
	}
	if err != nil {
		return nil, err
	}
	if kb == nil || strings.TrimSpace(kb.ID) != kbID || kb.TenantID != tenantID || !isManagedSmartArchiveKnowledgeBase(kb) {
		return nil, errors.New("smart archive target knowledge base is not managed")
	}
	return kb, nil
}

// withSmartArchiveKnowledgeWrite admits one internal mutation against the
// exact managed archive KB. Callers must supply the server-loaded KB returned
// by resolveManagedArchiveKnowledgeBase; the knowledge service also requires
// an explicit KB write grant, so carrying both facts keeps the bridge narrow
// and preserves API-key scope checks.
func withSmartArchiveKnowledgeWrite(ctx context.Context, kb *types.KnowledgeBase, tenantID uint64) (context.Context, error) {
	if kb == nil || strings.TrimSpace(kb.ID) == "" || tenantID == 0 || kb.TenantID != tenantID || !isManagedSmartArchiveKnowledgeBase(kb) {
		return ctx, errors.New("smart archive managed knowledge base context is unavailable")
	}
	return access.WithKBTaskWrite(
		withSmartArchiveMutation(ctx),
		kb,
		tenantID,
	)
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
