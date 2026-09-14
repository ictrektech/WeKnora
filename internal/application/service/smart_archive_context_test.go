package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type smartArchiveMirrorTestRepo struct {
	interfaces.ArchiveRepository
	settings *types.ArchiveSettings
}

func (r *smartArchiveMirrorTestRepo) GetSettings(context.Context, uint64) (*types.ArchiveSettings, error) {
	return r.settings, nil
}

type smartArchiveMirrorTestKBRepo struct {
	interfaces.KnowledgeBaseRepository
	kb  *types.KnowledgeBase
	err error
}

func (r *smartArchiveMirrorTestKBRepo) GetKnowledgeBaseByIDAndTenant(context.Context, string, uint64) (*types.KnowledgeBase, error) {
	return r.kb, r.err
}

type smartArchiveMirrorTestKnowledge struct {
	interfaces.KnowledgeService
	knowledge *types.Knowledge
	deletedID string
}

func (k *smartArchiveMirrorTestKnowledge) GetKnowledgeByID(context.Context, string) (*types.Knowledge, error) {
	return k.knowledge, nil
}

func (k *smartArchiveMirrorTestKnowledge) DeleteKnowledge(ctx context.Context, id string) error {
	k.deletedID = id
	return access.RequireKBWrite(ctx, &types.KnowledgeBase{
		ID:       k.knowledge.KnowledgeBaseID,
		TenantID: k.knowledge.TenantID,
	})
}

var (
	_ interfaces.ArchiveRepository = (*smartArchiveMirrorTestRepo)(nil)
	_ interfaces.KnowledgeService  = (*smartArchiveMirrorTestKnowledge)(nil)
)

func TestWithSmartArchiveKnowledgeWriteIsBoundToManagedKnowledgeBase(t *testing.T) {
	kb := &types.KnowledgeBase{
		ID:          "managed-kb",
		TenantID:    7,
		Description: types.ManagedSmartArchiveKnowledgeBaseMarker + " test",
	}
	ctx, err := withSmartArchiveKnowledgeWrite(context.Background(), kb, 7)
	require.NoError(t, err)
	require.True(t, isSmartArchiveMutation(ctx))
	require.NoError(t, access.RequireKBWrite(ctx, &types.KnowledgeBase{ID: "managed-kb", TenantID: 7}))
	require.ErrorIs(t, access.RequireKBWrite(ctx, &types.KnowledgeBase{ID: "other-kb", TenantID: 7}), access.ErrForbidden)
	require.ErrorIs(t, access.RequireKBWrite(ctx, &types.KnowledgeBase{ID: "managed-kb", TenantID: 8}), access.ErrForbidden)
	_, err = withSmartArchiveKnowledgeWrite(context.Background(), &types.KnowledgeBase{ID: "ordinary-kb", TenantID: 7}, 7)
	require.Error(t, err)
	_, err = withSmartArchiveKnowledgeWrite(context.Background(), &types.KnowledgeBase{ID: "managed-kb", TenantID: 8, Description: types.ManagedSmartArchiveKnowledgeBaseMarker}, 7)
	require.Error(t, err)
}

func TestDeleteManagedKnowledgeMirrorUsesManagedKnowledgeWriteGrant(t *testing.T) {
	const tenantID uint64 = 7
	const archiveDocumentID = "archive-document"

	repo := &smartArchiveMirrorTestRepo{settings: &types.ArchiveSettings{
		TenantID:               tenantID,
		ManagedKnowledgeBaseID: "managed-kb",
	}}
	knowledge := &smartArchiveMirrorTestKnowledge{knowledge: &types.Knowledge{
		ID:              "mirror-knowledge",
		TenantID:        tenantID,
		KnowledgeBaseID: "managed-kb",
		Metadata:        types.JSON([]byte(`{"source":"smart_archive","archive_document_id":"` + archiveDocumentID + `"}`)),
	}}
	kbRepo := &smartArchiveMirrorTestKBRepo{kb: &types.KnowledgeBase{
		ID:          "managed-kb",
		TenantID:    tenantID,
		Description: types.ManagedSmartArchiveKnowledgeBaseMarker + " test",
	}}
	svc := &smartArchiveService{repo: repo, kbRepo: kbRepo, knowledge: knowledge}
	ctx := types.WithCaller(context.Background(), types.Caller{
		TenantID: tenantID,
		UserID:   "admin",
		Role:     types.TenantRoleAdmin,
	})
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: tenantID})
	doc := &types.ArchiveDocument{ID: archiveDocumentID, TenantID: tenantID, KnowledgeID: knowledge.knowledge.ID}

	require.NoError(t, svc.deleteManagedKnowledgeMirror(ctx, tenantID, doc))
	require.Equal(t, knowledge.knowledge.ID, knowledge.deletedID)
	require.Empty(t, doc.KnowledgeID)
}

func TestDeleteManagedKnowledgeMirrorRejectsUnmanagedKnowledgeBase(t *testing.T) {
	const tenantID uint64 = 7
	const archiveDocumentID = "archive-document"

	repo := &smartArchiveMirrorTestRepo{settings: &types.ArchiveSettings{
		TenantID:               tenantID,
		ManagedKnowledgeBaseID: "ordinary-kb",
	}}
	knowledge := &smartArchiveMirrorTestKnowledge{knowledge: &types.Knowledge{
		ID:              "mirror-knowledge",
		TenantID:        tenantID,
		KnowledgeBaseID: "ordinary-kb",
		Metadata:        types.JSON([]byte(`{"source":"smart_archive","archive_document_id":"` + archiveDocumentID + `"}`)),
	}}
	kbRepo := &smartArchiveMirrorTestKBRepo{kb: &types.KnowledgeBase{
		ID:       "ordinary-kb",
		TenantID: tenantID,
	}}
	svc := &smartArchiveService{repo: repo, kbRepo: kbRepo, knowledge: knowledge}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: tenantID, UserID: "admin", Role: types.TenantRoleAdmin})
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: tenantID})
	doc := &types.ArchiveDocument{ID: archiveDocumentID, TenantID: tenantID, KnowledgeID: knowledge.knowledge.ID}

	require.Error(t, svc.deleteManagedKnowledgeMirror(ctx, tenantID, doc))
	require.Empty(t, knowledge.deletedID)
	require.Equal(t, knowledge.knowledge.ID, doc.KnowledgeID)
}
