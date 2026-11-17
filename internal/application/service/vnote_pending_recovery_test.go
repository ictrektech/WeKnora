package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type vnoteRecoveryInspector struct {
	interfaces.TaskInspector
	interfaces.RuntimeTaskInspector
	info        *types.RuntimeTaskInfo
	err         error
	runs, reads int
	unsupported bool
}

func (i *vnoteRecoveryInspector) GetRuntimeTask(context.Context, string, string) (*types.RuntimeTaskInfo, bool, error) {
	i.reads++
	if i.err != nil {
		return nil, !i.unsupported, i.err
	}
	if i.info == nil {
		return nil, true, asynq.ErrTaskNotFound
	}
	return i.info, true, nil
}
func (i *vnoteRecoveryInspector) RunRuntimeTask(_ context.Context, queue, id string) (bool, error) {
	if i.info == nil || i.info.ID != id || queue != types.QueueDefault {
		return true, errors.New("wrong task")
	}
	i.runs++
	i.info.State = types.RuntimeTaskPending
	return true, nil
}

func TestVNotePendingRecoveryPreservesAttemptAndNeverReplaysProcessing(t *testing.T) {
	sync, db, fake, ctx, source, id := syncServiceFixture(t)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}, &types.KnowledgeTagRelation{}))
	queue := &vnoteTaskCapture{}
	inspector := &vnoteRecoveryInspector{}
	manual := &knowledgeService{repo: fake.repo, kbService: sync.imports.knowledgeBases, task: queue, taskInspector: inspector, fileSvc: &documentFileSpy{}, tagRepo: repository.NewKnowledgeTagRepository(db)}
	sync.imports.knowledge = manual
	require.NoError(t, sync.Step(context.Background(), id))
	desired := syncDesired(t, db, id)
	require.Equal(t, "indexing", desired.Status)
	require.Len(t, queue.tasks, 1)
	original := queue.tasks[0]
	queue.tasks = nil // the persisted pending row survives a lost queue backend
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", desired.KnowledgeID).Update("updated_at", time.Now().UTC().Add(-2*time.Minute)).Error)
	wakeSync(t, db, id, false)
	require.NoError(t, sync.Step(context.Background(), id))
	require.Len(t, queue.tasks, 1)
	require.JSONEq(t, string(original.Payload()), string(queue.tasks[0].Payload()))
	require.Equal(t, 1, source.exports, "repair uses the stored authorized payload, not another body export")
	after := syncDesired(t, db, id)
	require.Equal(t, desired.Attempt, after.Attempt)
	require.Equal(t, desired.KnowledgeID, after.KnowledgeID)
	var generations int64
	require.NoError(t, db.Model(&types.VNoteImportGeneration{}).Count(&generations).Error)
	require.EqualValues(t, 1, generations)
	inspector.info = &types.RuntimeTaskInfo{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(desired.KnowledgeID+desired.Attempt)).String(), Type: types.TypeManualProcess, State: types.RuntimeTaskPending, TenantID: 7, KnowledgeID: desired.KnowledgeID}
	wakeSync(t, db, id, false)
	require.NoError(t, sync.Step(context.Background(), id))
	require.Len(t, queue.tasks, 1, "healthy queued work is neither duplicated nor cancelled")
	inspector.info.State = types.RuntimeTaskArchived
	require.NoError(t, manual.RecoverVNotePending(ctx, desired.KnowledgeID, desired.Attempt))
	require.Equal(t, 1, inspector.runs, "archived pending delivery restores its exact retained task")
	manual.taskInspector = nil // Lite has no persistent/inspectable task backend.
	queue.tasks = nil
	require.NoError(t, manual.RecoverVNotePending(ctx, desired.KnowledgeID, desired.Attempt))
	require.Len(t, queue.tasks, 1)
	manual.taskInspector = inspector
	// If another worker has claimed the document, even a missing queue cannot replay it.
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", desired.KnowledgeID).Update("parse_status", types.ParseStatusProcessing).Error)
	inspector.info = nil
	require.NoError(t, manual.RecoverVNotePending(ctx, desired.KnowledgeID, desired.Attempt))
	require.Len(t, queue.tasks, 1)
	var payload types.ManualProcessPayload
	require.NoError(t, json.Unmarshal(queue.tasks[0].Payload(), &payload))
	manual.tenantRepo = &documentTenantSpy{}
	require.NoError(t, manual.ProcessManualUpdate(context.Background(), queue.tasks[0]), "a recovered duplicate exits before any model or cleanup")
	require.Equal(t, desired.Attempt, payload.VNoteAttempt)
}

func TestVNotePendingRecoveryFailsClosedAndDoesNotOverrideBackoff(t *testing.T) {
	for _, state := range []types.RuntimeTaskState{types.RuntimeTaskPending, types.RuntimeTaskActive, types.RuntimeTaskRetry, types.RuntimeTaskScheduled} {
		t.Run(string(state), func(t *testing.T) {
			id, attempt := "knowledge", "attempt"
			inspector := &vnoteRecoveryInspector{info: &types.RuntimeTaskInfo{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(id+attempt)).String(), Type: types.TypeManualProcess, State: state, TenantID: 7, KnowledgeID: id}}
			svc := &knowledgeService{taskInspector: inspector} // no repository: an existing task must not read a body
			ctx := types.WithCaller(context.Background(), types.Caller{UserID: "alice", TenantID: 7, Role: types.TenantRoleOwner})
			require.NoError(t, svc.RecoverVNotePending(ctx, id, attempt))
			require.Zero(t, inspector.runs)
		})
	}
	sync, db, fake, ctx, source, id := syncServiceFixture(t)
	require.NoError(t, sync.Step(context.Background(), id))
	note := syncDesired(t, db, id)
	queue := &vnoteTaskCapture{}
	inspector := &vnoteRecoveryInspector{err: errors.New("queue unavailable")}
	manual := &knowledgeService{repo: fake.repo, task: queue, taskInspector: inspector}
	require.Error(t, manual.RecoverVNotePending(ctx, note.KnowledgeID, note.Attempt))
	require.Empty(t, queue.tasks, "inspection errors are not evidence of a missing task")
	inspector.unsupported = true
	require.Error(t, manual.RecoverVNotePending(ctx, note.KnowledgeID, note.Attempt))
	require.Empty(t, queue.tasks, "a backend error cannot turn an unavailable inspector into Lite mode")
	inspector.unsupported = false
	inspector.err = nil
	source.active = false
	require.Error(t, manual.RecoverVNotePending(ctx, note.KnowledgeID, note.Attempt))
	require.Empty(t, queue.tasks)
	source.active = true
	source.paused = true
	require.Error(t, manual.RecoverVNotePending(ctx, note.KnowledgeID, note.Attempt))
	require.Empty(t, queue.tasks)
	source.paused = false
	require.NoError(t, repository.ApplyVNoteControl(ctx, db, "alice", "personal", "subject", 2, false, []string{"root"}))
	require.Error(t, manual.RecoverVNotePending(ctx, note.KnowledgeID, note.Attempt), "a pending snapshot cannot bypass the current control")
	require.Empty(t, queue.tasks)
	require.NoError(t, sync.imports.ApplyControl(ctx, "subject", "personal", 2, false, []string{"root"}))
	require.NoError(t, manual.RecoverVNotePending(ctx, note.KnowledgeID, note.Attempt))
	require.Empty(t, queue.tasks)
}
