package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVNoteEmptyAndManualRootsProvisionWithoutExportOrModel(t *testing.T) {
	for _, mode := range []string{"interval", "manual"} {
		t.Run(mode, func(t *testing.T) {
			svc, db, knowledge, ctx, state, id := syncServiceFixture(t)
			state.deleted = true
			state.schedule.Mode = mode
			require.NoError(t, svc.Step(context.Background(), id))
			var root types.VNoteSyncRoot
			require.NoError(t, db.First(&root, "id=?", id).Error)
			kbID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vnote-kb\x00"+root.UserID+"\x00"+root.RootID)).String()
			kb, err := svc.imports.knowledgeBases.GetKnowledgeBaseByID(ctx, kbID)
			require.NoError(t, err)
			require.Equal(t, "alice", kb.PrivateOwnerID)
			require.Equal(t, "root", kb.VNoteRootID)
			require.Zero(t, state.exports)
			require.Zero(t, knowledge.published)
			var docs int64
			require.NoError(t, db.Model(&types.Knowledge{}).Count(&docs).Error)
			require.Zero(t, docs)
			// Even manual-only roots follow metadata renames without scanning notes.
			state.rootName = strings.Repeat("笔", 300)
			wakeSync(t, db, id, true)
			require.NoError(t, svc.Step(context.Background(), id))
			kb, err = svc.imports.knowledgeBases.GetKnowledgeBaseByID(ctx, kb.ID)
			require.NoError(t, err)
			require.Equal(t, "V-Note / "+strings.Repeat("笔", 246), kb.Name)
			require.Zero(t, knowledge.published)
		})
	}
}

func TestVNoteMetadataCreationAndRenameRejectLateControl(t *testing.T) {
	svc, db, _, ctx, snapshot := vnoteFixture(t)
	scope := vnoteRootScope(ctx, snapshot.WorkspaceID, snapshot.Subject, snapshot.RootID, 1)
	require.NoError(t, svc.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 2, false, []string{"root"}))
	_, err := svc.ensureVNoteKnowledgeBase(ctx, scope, "Late")
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, svc.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 3, true, []string{"root"}))
	scope.ConfigVersion = 3
	kb, err := svc.ensureVNoteKnowledgeBase(ctx, scope, "Current")
	require.NoError(t, err)
	require.NoError(t, svc.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 4, false, []string{"root"}))
	err = repository.RenameVNoteKnowledgeBase(ctx, db, scope, kb, "Late rename")
	require.ErrorIs(t, err, repository.ErrVNoteControlStale)
	require.NoError(t, db.First(kb, "id=?", kb.ID).Error)
	require.Equal(t, "V-Note / Current", kb.Name)
	// Metadata provisioning cannot resurrect a soft-deleted root either.
	require.NoError(t, db.Delete(kb).Error)
	require.NoError(t, svc.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 5, true, []string{"root"}))
	scope.ConfigVersion = 5
	_, err = svc.ensureVNoteKnowledgeBase(ctx, scope, "Resurrect")
	require.ErrorIs(t, err, repository.ErrKnowledgeBaseNotFound)
}

func TestVNoteUnchangedManualImportRenamesOnlyKnowledgeBase(t *testing.T) {
	svc, db, knowledge, ctx, snapshot := vnoteFixture(t)
	first, err := svc.Import(ctx, snapshot)
	require.NoError(t, err)
	finishVNoteGeneration(t, db, ctx, first.KnowledgeID)
	snapshot.RootName = "Renamed notebook"
	result, err := svc.Import(ctx, snapshot)
	require.NoError(t, err)
	require.Equal(t, first.KnowledgeID, result.KnowledgeID)
	require.Equal(t, 1, knowledge.published)
	kb, err := svc.knowledgeBases.GetKnowledgeBaseByID(ctx, first.KnowledgeBaseID)
	require.NoError(t, err)
	require.Equal(t, "V-Note / Renamed notebook", kb.Name)
}
