package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

func vnoteRootScope(ctx context.Context, workspace, subject, root string, version int64) types.VNoteRootScope {
	caller := types.CallerFromContext(ctx)
	return types.VNoteRootScope{UserID: caller.UserID, TenantID: caller.TenantID, WorkspaceID: workspace, Subject: subject, RootID: root, ConfigVersion: version}
}

// Provisioning is metadata-only: an empty or manual-only root still gets its
// own private KB, without taking the model gate or enqueueing a document.
func (s *VNoteImportService) ensureVNoteKnowledgeBase(ctx context.Context, scope types.VNoteRootScope, name string) (*types.KnowledgeBase, error) {
	caller := types.CallerFromContext(ctx)
	if caller.UserID != scope.UserID || caller.TenantID != scope.TenantID || !caller.Role.HasPermission(types.TenantRoleContributor) {
		return nil, access.ErrForbidden
	}
	kbID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-kb\x00"+scope.UserID+"\x00"+scope.RootID)).String()
	kb, err := s.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID)
	if errors.Is(err, repository.ErrKnowledgeBaseNotFound) {
		embeddingID, modelErr := s.defaultEmbedding(ctx)
		if modelErr != nil {
			return nil, modelErr
		}
		kb = &types.KnowledgeBase{ID: kbID, Name: types.VNoteKnowledgeBaseName(name), Type: types.KnowledgeBaseTypeDocument,
			TenantID: scope.TenantID, PrivateOwnerID: scope.UserID, VNoteRootID: scope.RootID, EmbeddingModelID: embeddingID,
			IndexingStrategy:         types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true},
			QuestionGenerationConfig: &types.QuestionGenerationConfig{Enabled: false}, AutoTagConfig: &types.AutoTagConfig{Enabled: false}}
		kb, err = s.knowledgeBases.CreateKnowledgeBase(types.WithVNoteRootScope(ctx, scope), kb)
		if err != nil {
			// Concurrent manual/background provisioning may have won. Never
			// treat a deleted row or a mismatched owner as successful creation.
			kb, err = s.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID)
		}
	}
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.PrivateOwnerID != scope.UserID || kb.VNoteRootID != scope.RootID || kb.TenantID != scope.TenantID {
		return nil, access.ErrForbidden
	}
	if kb.Name != types.VNoteKnowledgeBaseName(name) {
		if err := repository.RenameVNoteKnowledgeBase(ctx, s.database, scope, kb, name); err != nil {
			return nil, err
		}
		kb.Name = types.VNoteKnowledgeBaseName(name)
	}
	return kb, nil
}
