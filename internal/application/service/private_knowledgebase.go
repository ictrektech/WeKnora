package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

type vnoteMutationKey struct{}

type vnoteMutationScope struct{ knowledgeBaseID, userID string }

func withVNoteMutation(ctx context.Context, kb *types.KnowledgeBase) (context.Context, error) {
	if kb == nil || kb.ID == "" || kb.VNoteRootID == "" || kb.PrivateOwnerID == "" || !access.CanReadPrivateKB(ctx, kb) {
		return ctx, access.ErrForbidden
	}
	scoped := context.WithValue(ctx, vnoteMutationKey{}, vnoteMutationScope{kb.ID, kb.PrivateOwnerID})
	return access.WithKBTaskWrite(scoped, kb, kb.TenantID)
}

func rejectVNoteMutation(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil || kb.VNoteRootID == "" {
		return nil
	}
	scope, authorized := ctx.Value(vnoteMutationKey{}).(vnoteMutationScope)
	if !authorized || scope.knowledgeBaseID != kb.ID || scope.userID != kb.PrivateOwnerID || !access.CanReadPrivateKB(ctx, kb) {
		return errors.New("V-Note documents must be maintained through the V-Note integration")
	}
	return nil
}

func (s *kbShareService) CanReadPrivateKnowledgeBase(ctx context.Context, id string) (bool, error) {
	if s.kbRepo == nil {
		return false, errors.New("knowledge base visibility is unavailable")
	}
	kb, err := s.kbRepo.GetKnowledgeBaseByID(ctx, id)
	if errors.Is(err, repository.ErrKnowledgeBaseNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return access.CanReadPrivateKB(ctx, kb), nil
}
