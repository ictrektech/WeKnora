package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPatentSessionFilterBeforePaginationAndCount(t *testing.T) {
	repo, db := newSessionRepositoryForTest(t)
	require.NoError(t, db.AutoMigrate(&testIMChannelSession{}))
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePlatform, types.WorkspaceModeLegalAssistant, types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		for i := 0; i < 3; i++ {
			require.NoError(t, db.Create(&types.Session{TenantID: 1, UserID: "alice", WorkspaceMode: mode}).Error)
		}
	}
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePlatform, types.WorkspaceModeLegalAssistant, types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		rows, count, err := repo.QueryPaged(context.Background(), &types.SessionListQuery{TenantID: 1, UserID: "alice", WorkspaceMode: mode, Page: 2, PageSize: 2})
		require.NoError(t, err)
		require.EqualValues(t, 3, count)
		require.Len(t, rows, 1)
		require.Equal(t, mode, rows[0].WorkspaceMode)
	}
	rows, count, err := repo.QueryPaged(context.Background(), &types.SessionListQuery{TenantID: 1, UserID: "alice", HidePatentSessions: true, Page: 1, PageSize: 30})
	require.NoError(t, err)
	require.EqualValues(t, 6, count)
	for _, row := range rows {
		require.False(t, row.WorkspaceMode.IsPatent())
	}
}

func TestPatentArtifactsHiddenBeforePaginationAndCount(t *testing.T) {
	db := newArtifactTestDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePlatform, types.WorkspaceModeLegalAssistant, types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		sessionID := createSession(t, db, &types.Session{TenantID: 1, UserID: "alice", WorkspaceMode: mode})
		_, err := repo.CreateMessage(ctx, &types.Message{SessionID: sessionID, Role: "assistant", Artifacts: types.MessageArtifacts{testArtifact(string(mode)+".txt", "/result.txt", time.Now())}})
		require.NoError(t, err)
	}
	rows, total, err := repo.ListArtifactLibrary(ctx, &types.ArtifactLibraryQuery{TenantID: 1, UserID: "alice", Page: 2, PageSize: 1,
		HiddenWorkspaceModes: []types.WorkspaceMode{types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting}})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, rows, 1)
	require.NotContains(t, rows[0].FileName, "patent")
}
