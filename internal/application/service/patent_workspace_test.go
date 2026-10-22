package service

import (
	"context"
	"errors"
	"testing"
	"time"

	files "github.com/Tencent/WeKnora/internal/application/service/file"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestPatentSessionAccessAndOwnerIsolation(t *testing.T) {
	svc, db := newTestSessionService(t)
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		row := &types.Session{TenantID: 1, UserID: "alice", WorkspaceMode: mode}
		require.NoError(t, db.Create(row).Error)
		tenant := &types.Tenant{ID: 1, PatentWorkspaceConfig: &types.PatentWorkspaceConfig{Enabled: false}}
		ctx := context.WithValue(testSessionScopeContext(1, "alice"), types.TenantInfoContextKey, tenant)
		_, err := svc.GetSession(ctx, row.ID)
		require.ErrorIs(t, err, apperrors.ErrPatentWorkspaceDisabled)
		tenant.PatentWorkspaceConfig.Enabled = true
		_, err = svc.GetSession(ctx, row.ID)
		require.NoError(t, err)
		require.NoError(t, svc.UpdateSession(ctx, &types.Session{ID: row.ID, TenantID: 1, WorkspaceMode: types.WorkspaceModePlatform, Title: "renamed"}))
		saved, err := svc.GetSession(ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, mode, saved.WorkspaceMode, "metadata updates cannot move sessions")
		_, err = svc.GetSession(testSessionScopeContext(1, "bob"), row.ID)
		require.ErrorIs(t, err, apperrors.ErrSessionNotFound)
		_, err = svc.GetSession(testSessionScopeContext(2, "alice"), row.ID)
		require.ErrorIs(t, err, apperrors.ErrSessionNotFound)
	}
}

func TestPatentSessionPurgePreservesOtherWorkspaces(t *testing.T) {
	svc, db := newTestSessionService(t)
	for _, mode := range []types.WorkspaceMode{types.WorkspaceModePlatform, types.WorkspaceModeLegalAssistant, types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting} {
		require.NoError(t, db.Create(&types.Session{TenantID: 1, UserID: "alice", WorkspaceMode: mode}).Error)
	}
	require.NoError(t, db.Create(&types.Session{TenantID: 2, UserID: "alice", WorkspaceMode: types.WorkspaceModePatentSearch}).Error)
	archived := &types.Session{TenantID: 1, UserID: "alice", WorkspaceMode: types.WorkspaceModePatentSearch}
	require.NoError(t, db.Create(archived).Error)
	require.NoError(t, db.Delete(archived).Error)
	snapshot, err := svc.ListPatentSessions(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, snapshot, 3)
	late := &types.Session{TenantID: 1, UserID: "alice", WorkspaceMode: types.WorkspaceModePatentSearch}
	require.NoError(t, db.Create(late).Error)
	require.NoError(t, svc.DeletePatentSessions(context.Background(), 1, snapshot))
	var rows []*types.Session
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 4)
	for _, row := range rows {
		require.True(t, row.TenantID != 1 || !row.WorkspaceMode.IsPatent() || row.ID == late.ID)
	}
}

type patentPurgeSessions struct {
	interfaces.SessionService
	deleted bool
}

func (s *patentPurgeSessions) ListPatentSessions(context.Context, uint64) ([]*types.Session, error) {
	return []*types.Session{{ID: "patent", WorkspaceMode: types.WorkspaceModePatentSearch}}, nil
}
func (s *patentPurgeSessions) DeletePatentSessions(context.Context, uint64, []*types.Session) error {
	s.deleted = true
	return nil
}

type patentPurgeDocuments struct {
	interfaces.TemporaryDocumentService
	fail  bool
	calls int
}

func (s *patentPurgeDocuments) List(context.Context, uint64, string) ([]*types.TemporaryDocument, error) {
	return []*types.TemporaryDocument{{ID: "attachment"}}, nil
}
func (s *patentPurgeDocuments) Delete(context.Context, uint64, string, string) error {
	s.calls++
	if s.fail {
		return errors.New("storage unavailable")
	}
	return nil
}

type emptyPatentMessages struct{ interfaces.MessageRepository }

func (s emptyPatentMessages) GetMessagesBySession(context.Context, string, int, int) ([]*types.Message, error) {
	return nil, nil
}
func (s emptyPatentMessages) SoftDeleteSessionArtifacts(context.Context, string, []types.ArtifactRef, time.Time) ([]types.ArtifactRef, error) {
	return nil, nil
}

func TestPatentPurgeStorageFailureLeavesSessionsForRetry(t *testing.T) {
	sessions := &patentPurgeSessions{}
	documents := &patentPurgeDocuments{fail: true}
	svc := NewPatentWorkspaceDataService(sessions, documents, emptyPatentMessages{}, &contractReviewPurgeFileStub{}, &fakeCatalog{})
	require.Error(t, svc.DeleteTenantData(context.Background(), 1))
	require.False(t, sessions.deleted)
	documents.fail = false
	require.NoError(t, svc.DeleteTenantData(context.Background(), 1))
	require.True(t, sessions.deleted)
	require.Equal(t, 2, documents.calls)
}

type patentMessageResources struct {
	interfaces.MessageRepository
	message *types.Message
	live    int64
}

func (s *patentMessageResources) GetMessagesBySession(context.Context, string, int, int) ([]*types.Message, error) {
	return []*types.Message{s.message}, nil
}
func (s *patentMessageResources) SoftDeleteSessionArtifacts(_ context.Context, _ string, refs []types.ArtifactRef, at time.Time) ([]types.ArtifactRef, error) {
	for i := range s.message.Artifacts {
		s.message.Artifacts[i].DeletedAt = &at
	}
	return refs, nil
}
func (s *patentMessageResources) CountLiveArtifactsByURL(context.Context, string) (int64, error) {
	return s.live, nil
}

type patentFailingFiles struct {
	interfaces.FileService
	fail bool
}

func (s *patentFailingFiles) DeleteFile(ctx context.Context, ref string) error {
	if s.fail {
		return errors.New("object storage offline")
	}
	return s.FileService.DeleteFile(ctx, ref)
}

func TestPatentPurgeResourceRetryAndSharedBindings(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "shared"}[shared], func(t *testing.T) {
			ctx := context.Background()
			catalog, _ := newResourceCatalogForTest(t)
			storage := files.NewResourceCatalogFileService(files.NewLocalFileService(t.TempDir(), ""), catalog)
			ref, err := storage.SaveBytes(ctx, []byte("patent result"), 1, "result.txt", false)
			require.NoError(t, err)
			require.NoError(t, catalog.Bind(ctx, ref, types.ResourceOwnerMessage, "message", types.ResourceRelationArtifact))
			if shared {
				require.NoError(t, catalog.Bind(ctx, ref, types.ResourceOwnerKnowledge, "shared-kb", types.ResourceRelationSourceFile))
			}
			messages := &patentMessageResources{message: &types.Message{ID: "message", Artifacts: types.MessageArtifacts{{URL: ref}}}}
			flaky := &patentFailingFiles{FileService: storage, fail: !shared}
			svc := &patentWorkspaceDataService{messages: messages, files: flaky, catalog: catalog}
			sessions := []*types.Session{{ID: "patent"}}
			if !shared {
				require.Error(t, svc.releaseMessageResources(ctx, sessions))
				require.NotEmpty(t, messages.message.Artifacts[0].URL, "retain the URL for retry")
				flaky.fail = false
			}
			require.NoError(t, svc.releaseMessageResources(ctx, sessions))
			// Repeating after partial progress must not fail on a previously deleted object.
			require.NoError(t, svc.releaseMessageResources(ctx, sessions))
			f, err := storage.GetFile(ctx, ref)
			if shared {
				require.NoError(t, err)
				require.NoError(t, f.Close())
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestPatentPurgeMissingRawFileIsIdempotent(t *testing.T) {
	ctx := context.Background()
	storage := files.NewLocalFileService(t.TempDir(), "")
	ref, err := storage.SaveBytes(ctx, []byte("result"), 1, "result.txt", false)
	require.NoError(t, err)
	require.NoError(t, deleteFileIfPresent(ctx, storage, nil, ref))
	require.NoError(t, deleteFileIfPresent(ctx, storage, nil, ref))
}
