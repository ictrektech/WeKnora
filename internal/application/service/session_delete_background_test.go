package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/background"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type recordingDeleteListener struct {
	deleted []interfaces.SessionRef
	aborted []interfaces.SessionRef
	err     error
}

func (l *recordingDeleteListener) OnStop(context.Context, interfaces.SessionRef) error { return nil }

func (l *recordingDeleteListener) OnDelete(_ context.Context, s interfaces.SessionRef) error {
	l.deleted = append(l.deleted, s)
	return l.err
}

func (l *recordingDeleteListener) OnDeleteAborted(_ context.Context, s interfaces.SessionRef) error {
	l.aborted = append(l.aborted, s)
	return nil
}

func newDeleteBackgroundFixture(t *testing.T, ids ...string) (*sessionService, *recordingDeleteListener) {
	t.Helper()
	svc, db, _ := newSessionServiceForForkDeleteTest(t)
	work := NewBackgroundWork(background.NewMemoryActivityRegistry(nil), background.NewMemoryNoticeInbox(nil))
	listener := &recordingDeleteListener{}
	work.Listeners.Register(listener)
	svc.background = work
	for _, id := range ids {
		require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&types.Session{
			ID: id, TenantID: 1, UserID: "u1", Title: id,
		}).Error)
		ref := interfaces.SessionRef{TenantID: 1, SessionID: id}
		require.NoError(t, work.Registry.Add(context.Background(), ref, interfaces.BackgroundActivity{
			ID: "sj_" + id, Kind: "shell_job", StartedAt: time.Now(),
		}, time.Hour, 0))
	}
	return svc, listener
}

func requireNoBackgroundLeft(t *testing.T, svc *sessionService, id string) {
	t.Helper()
	live, err := svc.background.Registry.List(context.Background(), interfaces.SessionRef{TenantID: 1, SessionID: id})
	require.NoError(t, err)
	require.Empty(t, live)
}

func TestDeleteSessionNotifiesBackgroundListeners(t *testing.T) {
	svc, listener := newDeleteBackgroundFixture(t, "s1")

	require.NoError(t, svc.DeleteSession(testSessionScopeContext(1, "u1"), "s1"))

	require.Equal(t, []interfaces.SessionRef{{TenantID: 1, SessionID: "s1"}}, listener.deleted)
	requireNoBackgroundLeft(t, svc, "s1")
}

func TestBatchDeleteSessionsNotifiesBackgroundListeners(t *testing.T) {
	svc, listener := newDeleteBackgroundFixture(t, "s1", "s2")

	require.NoError(t, svc.BatchDeleteSessions(testSessionScopeContext(1, "u1"), []string{"s1", "s2"}))

	require.ElementsMatch(t, []interfaces.SessionRef{
		{TenantID: 1, SessionID: "s1"}, {TenantID: 1, SessionID: "s2"},
	}, listener.deleted)
	requireNoBackgroundLeft(t, svc, "s2")
}

func TestDeleteAllSessionsNotifiesBackgroundListeners(t *testing.T) {
	svc, listener := newDeleteBackgroundFixture(t, "s1", "s2")

	require.NoError(t, svc.DeleteAllSessions(testSessionScopeContext(1, "u1")))

	require.Len(t, listener.deleted, 2)
	requireNoBackgroundLeft(t, svc, "s1")
}

func TestWorkspacePurgeStopsBackgroundBeforeDeletingSessions(t *testing.T) {
	for _, mode := range []types.WorkspaceMode{
		types.WorkspaceModeLegalAssistant, types.WorkspaceModePatentSearch, types.WorkspaceModePatentWriting,
	} {
		for _, stopFails := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: "/success", true: "/stop-failed"}[stopFails], func(t *testing.T) {
				svc, db := newTestSessionService(t)
				row := &types.Session{TenantID: 1, UserID: "u1", WorkspaceMode: mode}
				require.NoError(t, db.Create(row).Error)
				other := &types.Session{TenantID: 1, UserID: "u1", WorkspaceMode: types.WorkspaceModePlatform}
				require.NoError(t, db.Create(other).Error)
				work := NewBackgroundWork(background.NewMemoryActivityRegistry(nil), background.NewMemoryNoticeInbox(nil))
				listener := &recordingDeleteListener{}
				if stopFails {
					listener.err = errors.New("shell job is still stopping")
				}
				work.Listeners.Register(listener)
				svc.background = work
				ref := interfaces.SessionRef{TenantID: 1, SessionID: row.ID}
				require.NoError(t, work.Registry.Add(context.Background(), ref, interfaces.BackgroundActivity{
					ID: "sj_purge", Kind: "shell_job", StartedAt: time.Now(),
				}, time.Hour, 0))
				var err error
				if mode == types.WorkspaceModeLegalAssistant {
					err = svc.DeleteLegalAssistantSessions(context.Background(), 1)
				} else {
					err = svc.DeletePatentSessions(context.Background(), 1, []*types.Session{row})
				}
				require.Equal(t, []interfaces.SessionRef{ref}, listener.deleted)
				var remaining int64
				require.NoError(t, db.Model(&types.Session{}).Where("id = ?", row.ID).Count(&remaining).Error)
				if stopFails {
					require.ErrorIs(t, err, listener.err)
					require.EqualValues(t, 1, remaining)
					require.Equal(t, []interfaces.SessionRef{ref}, listener.aborted)
					live, listErr := work.Registry.List(context.Background(), ref)
					require.NoError(t, listErr)
					require.Len(t, live, 1)
				} else {
					require.NoError(t, err)
					require.Zero(t, remaining)
					requireNoBackgroundLeft(t, svc, row.ID)
				}
				require.NoError(t, db.First(&types.Session{}, "id = ?", other.ID).Error)
			})
		}
	}
}
