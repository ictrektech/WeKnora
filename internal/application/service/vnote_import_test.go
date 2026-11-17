package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type vnoteTestKBs struct {
	interfaces.KnowledgeBaseService
	repo interfaces.KnowledgeBaseRepository
}

func (s *vnoteTestKBs) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return s.repo.GetKnowledgeBaseByID(ctx, id)
}
func (s *vnoteTestKBs) CreateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return kb, s.repo.CreateKnowledgeBase(ctx, kb)
}

type vnoteTestModels struct {
	interfaces.ModelService
	models []*types.Model
}

func (s *vnoteTestModels) ListModels(context.Context) ([]*types.Model, error) { return s.models, nil }

type vnoteTestKnowledge struct {
	interfaces.KnowledgeService
	repo      interfaces.KnowledgeRepository
	published int
	last      *types.ManualKnowledgePayload
}

func (s *vnoteTestKnowledge) GetRepository() interfaces.KnowledgeRepository { return s.repo }
func (s *vnoteTestKnowledge) CreateKnowledgeFromManual(ctx context.Context, kbID string, payload *types.ManualKnowledgePayload, channel string) (*types.Knowledge, error) {
	k := &types.Knowledge{ID: payload.InternalID, TenantID: types.CallerFromContext(ctx).TenantID, KnowledgeBaseID: kbID, Title: payload.Title, Type: types.KnowledgeTypeManual, Channel: channel, ParseStatus: types.ManualKnowledgeStatusDraft}
	if err := k.SetManualMetadata(types.NewManualKnowledgeMetadata(payload.Content, payload.Status, 1)); err != nil {
		return nil, err
	}
	k.Metadata, _ = mergeKnowledgeMetadata(k.Metadata, payload.SourceMetadata)
	return k, s.repo.CreateKnowledge(ctx, k)
}
func (s *vnoteTestKnowledge) UpdateManualKnowledge(ctx context.Context, id string, payload *types.ManualKnowledgePayload) (*types.Knowledge, error) {
	k, err := s.repo.GetKnowledgeByID(ctx, types.CallerFromContext(ctx).TenantID, id)
	if err != nil {
		return nil, err
	}
	if err := k.SetManualMetadata(types.NewManualKnowledgeMetadata(payload.Content, payload.Status, 2)); err != nil {
		return nil, err
	}
	k.Metadata, _ = mergeKnowledgeMetadata(k.Metadata, payload.SourceMetadata)
	k.ParseStatus = types.ParseStatusPending
	if err := s.repo.UpdateKnowledge(ctx, k); err != nil {
		return nil, err
	}
	s.published++
	s.last = payload
	return k, nil
}

func vnoteFixture(t *testing.T) (*VNoteImportService, *gorm.DB, *vnoteTestKnowledge, context.Context, types.VNoteSnapshot) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.VNoteImportState{}, &types.VNoteImportGeneration{}, &types.VNoteImportGate{}, &repository.VNoteControl{}, &types.VNoteSyncRoot{}, &types.VNoteSyncNote{}))
	kbs := &vnoteTestKBs{repo: repository.NewKnowledgeBaseRepository(db)}
	knowledge := &vnoteTestKnowledge{repo: repository.NewKnowledgeRepository(db)}
	models := &vnoteTestModels{models: []*types.Model{{ID: "default-embedding", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, IsDefault: true}}}
	s := NewVNoteImportService(db, kbs, knowledge, models)
	ctx := types.WithPrincipal(types.WithCaller(context.Background(), types.Caller{UserID: "alice", TenantID: 7, Role: types.TenantRoleOwner}), types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
	snapshot := types.VNoteSnapshot{Protocol: 1, Subject: "subject", WorkspaceID: "personal", ConfigVersion: 1, RootID: "root", RootName: "Notebook", NoteID: "note", Title: "Title", Markdown: "正文 `a < b`", UpdatedAt: "2026-11-11T12:00:00Z"}
	require.NoError(t, s.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, snapshot.ConfigVersion, true, []string{"root"}))
	return s, db, knowledge, ctx, snapshot
}

func finishVNoteGeneration(t *testing.T, db *gorm.DB, ctx context.Context, id string) {
	t.Helper()
	var generation types.VNoteImportGeneration
	require.NoError(t, db.Where("knowledge_id = ?", id).First(&generation).Error)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", id).Update("parse_status", types.ParseStatusProcessing).Error)
	var doc types.Knowledge
	require.NoError(t, db.First(&doc, "id = ?", id).Error)
	doc.ParseStatus = types.ParseStatusCompleted
	require.NoError(t, repository.PublishVNoteGeneration(ctx, db, generation.Attempt, generation.ExpectedKnowledgeID, &doc))
}

func TestVNoteImportIdempotencyAndGlobalProcessingGate(t *testing.T) {
	s, db, knowledge, ctx, snapshot := vnoteFixture(t)
	first, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, first.State)
	require.NotEmpty(t, first.Attempt)
	status, err := s.Status(ctx, snapshot.RootID, snapshot.NoteID)
	require.NoError(t, err)
	require.Equal(t, first.Attempt, status.Attempt)
	require.Equal(t, 1, knowledge.published)
	kb, err := s.knowledgeBases.GetKnowledgeBaseByID(ctx, first.KnowledgeBaseID)
	require.NoError(t, err)
	require.Equal(t, "alice", kb.PrivateOwnerID)
	require.Equal(t, "root", kb.VNoteRootID)
	require.False(t, kb.QuestionGenerationConfig.Enabled)
	require.False(t, kb.AutoTagConfig.Enabled)
	require.False(t, kb.IndexingStrategy.WikiEnabled)
	require.Empty(t, kb.SummaryModelID)
	config := knowledge.last.ProcessConfig
	for _, off := range []*bool{config.SummaryEnabled, config.EnableMultimodel, config.GraphEnabled, config.ImageAttrsEnabled} {
		require.NotNil(t, off)
		require.False(t, *off)
	}
	require.False(t, config.QuestionGenerationConfig.Enabled)
	require.Equal(t, "# Title\n\n正文 `a < b`\n", knowledge.last.Content)
	require.Equal(t, "personal", knowledge.last.SourceMetadata["vnote_workspace_id"])
	require.Equal(t, "1", knowledge.last.SourceMetadata["vnote_config_version"])
	repeated, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.Equal(t, first, repeated)
	require.Equal(t, 1, knowledge.published, "unchanged pending note must not re-enqueue")
	other := snapshot
	other.NoteID = "other-note"
	_, err = s.Import(ctx, other)
	require.ErrorIs(t, err, ErrVNoteImportBusy)
	for _, status := range []string{types.ParseStatusProcessing, types.ParseStatusFinalizing} {
		require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", first.KnowledgeID).Update("parse_status", status).Error)
		_, err = s.Import(ctx, other)
		require.ErrorIs(t, err, ErrVNoteImportBusy)
	}
	finishVNoteGeneration(t, db, ctx, first.KnowledgeID)
	_, err = s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.Equal(t, 1, knowledge.published, "unchanged completed note must not re-embed")
	snapshot.Markdown = "Changed body"
	changed, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.NotEqual(t, first.KnowledgeID, changed.KnowledgeID, "changed content uses a separate physical generation")
	require.Equal(t, 2, knowledge.published)
	finishVNoteGeneration(t, db, ctx, changed.KnowledgeID)
	_, err = s.Import(ctx, other)
	require.NoError(t, err)
	require.Equal(t, 3, knowledge.published)
}

func TestVNoteEmbeddingSelection(t *testing.T) {
	builtin := func(id string) *types.Model {
		return &types.Model{ID: id, Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, Source: types.ModelSourceRemote, IsBuiltin: true, ManagedBy: "yaml"}
	}
	for _, tc := range []struct {
		name   string
		models []*types.Model
		want   string
	}{
		{"unique deployment model without flag", []*types.Model{nil, builtin("platform")}, "platform"},
		{"workspace overrides deployment", []*types.Model{{ID: "deployment", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, IsDefault: true}, {ID: "workspace", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, DefaultFor: []string{"Embedding"}}}, "workspace"},
		{"explicit user model", []*types.Model{builtin("platform"), {ID: "chosen", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, IsDefault: true}}, "chosen"},
		{"ambiguous deployment", []*types.Model{builtin("a"), builtin("b")}, ""},
		{"unselected custom provider", []*types.Model{{ID: "cloud", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, Source: types.ModelSourceOpenAI}}, ""},
		{"inactive or wrong capability", []*types.Model{{ID: "down", Type: types.ModelTypeEmbedding, Status: types.ModelStatusDownloading, IsDefault: true}, {ID: "chat", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive, IsDefault: true}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, knowledge, ctx, snapshot := vnoteFixture(t)
			s.models = &vnoteTestModels{models: tc.models}
			result, err := s.Import(ctx, snapshot)
			if tc.want == "" {
				require.ErrorIs(t, err, ErrVNoteEmbeddingUnavailable)
				require.Nil(t, result)
				require.Zero(t, knowledge.published)
				var count int64
				require.NoError(t, db.Model(&types.VNoteImportGeneration{}).Count(&count).Error)
				require.Zero(t, count, "a definite refusal must never stage a model task")
				// A fixed configuration can retry the same source version.
				s.models = &vnoteTestModels{models: []*types.Model{builtin("fixed")}}
				_, err = s.Import(ctx, snapshot)
				require.NoError(t, err)
				return
			}
			require.NoError(t, err)
			kb, err := s.knowledgeBases.GetKnowledgeBaseByID(ctx, result.KnowledgeBaseID)
			require.NoError(t, err)
			require.Equal(t, tc.want, kb.EmbeddingModelID)
		})
	}
}

func TestVNoteImportRejectsSnapshotWithoutVersionedPersonalScope(t *testing.T) {
	s, _, knowledge, ctx, snapshot := vnoteFixture(t)
	snapshot.ConfigVersion = 0
	_, err := s.Import(ctx, snapshot)
	require.Error(t, err)
	snapshot.ConfigVersion = 1
	snapshot.WorkspaceID = ""
	_, err = s.Import(ctx, snapshot)
	require.Error(t, err)
	require.Zero(t, knowledge.published)
}

func TestVNoteImportRejectsRevokedAndStaleScopes(t *testing.T) {
	s, _, knowledge, ctx, snapshot := vnoteFixture(t)
	require.NoError(t, s.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 2, false, []string{"root"}))
	_, err := s.Import(ctx, snapshot)
	require.ErrorIs(t, err, access.ErrForbidden)
	snapshot.ConfigVersion = 2
	_, err = s.Import(ctx, snapshot)
	require.ErrorIs(t, err, access.ErrForbidden)
	require.Zero(t, knowledge.published)
}

func TestVNoteImportNeedsModelAndCannotRecreateDeletedKB(t *testing.T) {
	s, db, knowledge, ctx, snapshot := vnoteFixture(t)
	s.models.(*vnoteTestModels).models = nil
	_, err := s.Import(ctx, snapshot)
	require.ErrorContains(t, err, "default embedding")
	require.Zero(t, knowledge.published)
	s.models.(*vnoteTestModels).models = []*types.Model{{ID: "embedding", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, IsDefault: true}}
	first, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.NoError(t, db.Where("id = ?", first.KnowledgeBaseID).Delete(&types.KnowledgeBase{}).Error)
	_, err = s.Import(ctx, snapshot)
	require.ErrorIs(t, err, repository.ErrKnowledgeBaseNotFound)
	require.Equal(t, 1, knowledge.published)
}

func TestVNoteManagedKBMutationRequiresExactSourceScope(t *testing.T) {
	_, _, _, ctx, _ := vnoteFixture(t)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7, PrivateOwnerID: "alice", VNoteRootID: "root"}
	require.Error(t, rejectVNoteMutation(ctx, kb))
	scoped, err := withVNoteMutation(ctx, kb)
	require.NoError(t, err)
	require.NoError(t, rejectVNoteMutation(scoped, kb))
	other := *kb
	other.ID = "other"
	require.Error(t, rejectVNoteMutation(scoped, &other))
	other.PrivateOwnerID = "bob"
	_, err = withVNoteMutation(ctx, &other)
	require.ErrorIs(t, err, access.ErrForbidden)
}

type vnoteTaskCapture struct{ tasks []*asynq.Task }

func (q *vnoteTaskCapture) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: "queued"}, nil
}

func TestVNoteImportUsesRealManualPublisherAndRejectsStaleWorker(t *testing.T) {
	s, db, fake, ctx, snapshot := vnoteFixture(t)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}, &types.KnowledgeTagRelation{}))
	s.knowledgeBases = &knowledgeBaseService{repo: repository.NewKnowledgeBaseRepository(db), modelService: s.models}
	queue := &vnoteTaskCapture{}
	manual := &knowledgeService{repo: fake.repo, kbService: s.knowledgeBases, task: queue, fileSvc: &documentFileSpy{}, tagRepo: repository.NewKnowledgeTagRepository(db)}
	s.knowledge = manual
	ctx = types.WithExecutionTenant(ctx, 7)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 7})
	first, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.Len(t, queue.tasks, 1)
	var task types.ManualProcessPayload
	require.NoError(t, json.Unmarshal(queue.tasks[0].Payload(), &task))
	require.NotEmpty(t, task.VNoteAttempt)
	require.Equal(t, first.KnowledgeID, task.KnowledgeID)
	stageCtx := types.WithVNoteAdmission(ctx, task.VNoteAttempt)
	doc, err := fake.repo.GetKnowledgeByID(stageCtx, 7, first.KnowledgeID)
	require.NoError(t, err)
	require.Equal(t, "vnote", doc.Channel)
	require.Equal(t, task.VNoteAttempt, doc.GetMetadata()["vnote_attempt"])
	require.Empty(t, doc.FilePath, "private content must not create a tenant-wide raw file")
	meta, err := doc.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, meta.Content, task.Content)
	_, err = s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.Len(t, queue.tasks, 1)
	// Keep the real repository and service entry point: a stale task must
	// return before any chunk cleanup, tracker or embedding dependencies.
	manual.tenantRepo = &documentTenantSpy{}
	stale := task
	stale.VNoteAttempt = "superseded"
	encoded, err := json.Marshal(stale)
	require.NoError(t, err)
	require.NoError(t, manual.ProcessManualUpdate(context.Background(), asynq.NewTask(types.TypeManualProcess, encoded)))
	doc, err = fake.repo.GetKnowledgeByID(stageCtx, 7, first.KnowledgeID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, doc.ParseStatus)
	admission := fake.repo.(interface {
		BeginVNoteProcessing(context.Context, string, string, string) (bool, error)
	})
	claimed, err := admission.BeginVNoteProcessing(ctx, doc.ID, doc.GetMetadata()["vnote_content_hash"], task.VNoteAttempt)
	require.NoError(t, err)
	require.True(t, claimed)
	originalMetadata := append([]byte(nil), doc.Metadata...)
	deniedMetadata, err := mergeKnowledgeMetadata(doc.Metadata, map[string]string{"vnote_authorization": "delegated"})
	require.NoError(t, err)
	// Even loss of authorization cannot let a duplicate delivery release a
	// processing slot owned by the real worker's outstanding model request.
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", doc.ID).Update("metadata", deniedMetadata).Error)
	require.NoError(t, manual.ProcessManualUpdate(context.Background(), queue.tasks[0]))
	doc, err = fake.repo.GetKnowledgeByID(stageCtx, 7, first.KnowledgeID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusProcessing, doc.ParseStatus, "duplicate worker must not reset or clean up the active run")
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", doc.ID).Update("metadata", originalMetadata).Error)
	// Exercise admission through the real worker and persist a model failure.
	// No model/network is called; the missing embedder is the controlled boundary.
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", doc.ID).Update("parse_status", types.ParseStatusPending).Error)
	manual.modelService = embedFailureModelService{err: errors.New("test embedding unavailable")}
	task.NeedCleanup = false
	encoded, err = json.Marshal(task)
	require.NoError(t, err)
	require.NoError(t, manual.ProcessManualUpdate(context.Background(), asynq.NewTask(types.TypeManualProcess, encoded)))
	doc, err = fake.repo.GetKnowledgeByID(stageCtx, 7, first.KnowledgeID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusProcessing, doc.ParseStatus, "a failed model request must not release an unconfirmed slot")
	require.Contains(t, doc.ErrorMessage, "test embedding unavailable")
	status, err := s.Status(ctx, snapshot.RootID, snapshot.NoteID)
	require.NoError(t, err)
	require.Equal(t, "recovery_required", status.State)
	claimed, err = admission.BeginVNoteProcessing(ctx, doc.ID, doc.GetMetadata()["vnote_content_hash"], task.VNoteAttempt)
	require.NoError(t, err)
	require.False(t, claimed, "a duplicate cannot reopen a terminal failure")
}

func TestVNoteRevocationBeforeRealWorkerClaimRetiresTask(t *testing.T) {
	s, db, fake, ctx, snapshot := vnoteFixture(t)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}, &types.KnowledgeTagRelation{}))
	s.knowledgeBases = &knowledgeBaseService{repo: repository.NewKnowledgeBaseRepository(db), modelService: s.models}
	queue := &vnoteTaskCapture{}
	manual := &knowledgeService{repo: fake.repo, kbService: s.knowledgeBases, task: queue, fileSvc: &documentFileSpy{}, tagRepo: repository.NewKnowledgeTagRepository(db), tenantRepo: &documentTenantSpy{}}
	s.knowledge = manual
	ctx = types.WithExecutionTenant(ctx, 7)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 7})
	result, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	require.Len(t, queue.tasks, 1)
	require.NoError(t, s.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 2, false, []string{"root"}))
	var retired types.Knowledge
	require.NoError(t, db.First(&retired, "id = ?", result.KnowledgeID).Error)
	require.Equal(t, types.ParseStatusCancelled, retired.ParseStatus)
	require.NoError(t, manual.ProcessManualUpdate(context.Background(), queue.tasks[0]))
	var doc types.Knowledge
	require.NoError(t, db.First(&doc, "id = ?", result.KnowledgeID).Error)
	require.Equal(t, types.ParseStatusCancelled, doc.ParseStatus)
}

func TestVNoteControlDoesNotRetireClaimedWork(t *testing.T) {
	s, db, knowledge, ctx, snapshot := vnoteFixture(t)
	result, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	var doc types.Knowledge
	require.NoError(t, db.First(&doc, "id = ?", result.KnowledgeID).Error)
	metadata := doc.GetMetadata()
	admission := knowledge.repo.(interface {
		BeginVNoteProcessing(context.Context, string, string, string) (bool, error)
	})
	claimed, err := admission.BeginVNoteProcessing(ctx, doc.ID, metadata["vnote_content_hash"], metadata["vnote_attempt"])
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, s.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 2, false, []string{"root"}))
	require.NoError(t, db.First(&doc, "id = ?", doc.ID).Error)
	require.Equal(t, types.ParseStatusProcessing, doc.ParseStatus)
	require.NoError(t, s.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 3, true, []string{"root"}))
	snapshot.ConfigVersion = 3
	_, err = s.Import(ctx, snapshot)
	require.ErrorIs(t, err, ErrVNoteImportBusy, "a revoked in-flight attempt cannot masquerade as the new version")
}

func TestVNoteRevocationDuringIndexKeepsPublishedGeneration(t *testing.T) {
	s, db, knowledge, ownerCtx, snapshot := vnoteFixture(t)
	old, err := s.Import(ownerCtx, snapshot)
	require.NoError(t, err)
	finishVNoteGeneration(t, db, ownerCtx, old.KnowledgeID)
	snapshot.Markdown = "new body"
	stage, err := s.Import(ownerCtx, snapshot)
	require.NoError(t, err)
	require.NotEqual(t, old.KnowledgeID, stage.KnowledgeID)
	visible, total, err := knowledge.repo.ListPagedKnowledgeByKnowledgeBaseID(ownerCtx, 7, old.KnowledgeBaseID, &types.Pagination{Page: 1, PageSize: 20}, types.KnowledgeListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, visible, 1)
	require.Equal(t, old.KnowledgeID, visible[0].ID)
	var generation types.VNoteImportGeneration
	require.NoError(t, db.Where("knowledge_id = ?", stage.KnowledgeID).First(&generation).Error)
	stageCtx := types.WithVNoteAdmission(ownerCtx, generation.Attempt)
	doc, err := knowledge.repo.GetKnowledgeByID(stageCtx, 7, stage.KnowledgeID)
	require.NoError(t, err)
	admission := knowledge.repo.(interface {
		BeginVNoteProcessing(context.Context, string, string, string) (bool, error)
		CheckVNoteProcessing(context.Context, string, string, string) error
	})
	claimed, err := admission.BeginVNoteProcessing(stageCtx, doc.ID, generation.ContentHash, generation.Attempt)
	require.NoError(t, err)
	require.True(t, claimed)
	doc.ParseStatus = types.ParseStatusProcessing
	stageCtx = types.WithVNoteWriteGuard(stageCtx, func(c context.Context) error {
		return admission.CheckVNoteProcessing(c, doc.ID, generation.ContentHash, generation.Attempt)
	})
	stageCtx, processor, _, _, chunks, engine, _ := chunkIndexWindowFixture(stageCtx, 1)
	processor.repo = knowledge.repo
	entered, release := make(chan struct{}), make(chan struct{})
	engine.afterIndex = func() { close(entered); <-release }
	kb, err := s.knowledgeBases.GetKnowledgeBaseByID(ownerCtx, old.KnowledgeBaseID)
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() { finished <- processor.processChunks(stageCtx, kb, doc, chunks) }()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("processing ended before index batch: %v", err)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("index batch did not start")
	}
	require.NoError(t, s.ApplyControl(ownerCtx, snapshot.Subject, snapshot.WorkspaceID, 2, false, []string{"root"}))
	close(release)
	<-finished
	var state types.VNoteImportState
	require.NoError(t, db.Where("id = ?", generation.StateID).First(&state).Error)
	require.Equal(t, old.KnowledgeID, state.KnowledgeID)
	_, err = knowledge.repo.GetKnowledgeByID(ownerCtx, 7, old.KnowledgeID)
	require.NoError(t, err, "the prior document remains readable")
	_, err = knowledge.repo.GetKnowledgeByID(ownerCtx, 7, stage.KnowledgeID)
	require.ErrorIs(t, err, repository.ErrKnowledgeNotFound)
	require.Empty(t, engine.indexed, "revoked staged vectors are removed after the batch returns")
}

func TestVNoteSuccessfulIndexAtomicallySwitchesVisibleGeneration(t *testing.T) {
	s, db, knowledge, ownerCtx, snapshot := vnoteFixture(t)
	old, err := s.Import(ownerCtx, snapshot)
	require.NoError(t, err)
	finishVNoteGeneration(t, db, ownerCtx, old.KnowledgeID)
	snapshot.Markdown = "new body"
	stage, err := s.Import(ownerCtx, snapshot)
	require.NoError(t, err)
	var generation types.VNoteImportGeneration
	require.NoError(t, db.Where("knowledge_id = ?", stage.KnowledgeID).First(&generation).Error)
	stageCtx := types.WithVNoteAdmission(ownerCtx, generation.Attempt)
	doc, err := knowledge.repo.GetKnowledgeByID(stageCtx, 7, stage.KnowledgeID)
	require.NoError(t, err)
	admission := knowledge.repo.(interface {
		BeginVNoteProcessing(context.Context, string, string, string) (bool, error)
		CheckVNoteProcessing(context.Context, string, string, string) error
	})
	claimed, err := admission.BeginVNoteProcessing(stageCtx, doc.ID, generation.ContentHash, generation.Attempt)
	require.NoError(t, err)
	require.True(t, claimed)
	doc.ParseStatus = types.ParseStatusProcessing
	stageCtx = types.WithVNoteWriteGuard(stageCtx, func(c context.Context) error {
		return admission.CheckVNoteProcessing(c, doc.ID, generation.ContentHash, generation.Attempt)
	})
	stageCtx, processor, _, _, chunks, _, _ := chunkIndexWindowFixture(stageCtx, 1)
	processor.repo = knowledge.repo
	kb, err := s.knowledgeBases.GetKnowledgeBaseByID(ownerCtx, old.KnowledgeBaseID)
	require.NoError(t, err)
	require.NoError(t, processor.processChunks(stageCtx, kb, doc, chunks))
	var state types.VNoteImportState
	require.NoError(t, db.Where("id = ?", generation.StateID).First(&state).Error)
	require.Equal(t, stage.KnowledgeID, state.KnowledgeID)
	_, err = knowledge.repo.GetKnowledgeByID(ownerCtx, 7, stage.KnowledgeID)
	require.NoError(t, err)
	resolved, err := knowledge.repo.GetKnowledgeByID(ownerCtx, 7, old.KnowledgeID)
	require.NoError(t, err)
	require.Equal(t, stage.KnowledgeID, resolved.ID, "prior published IDs resolve to the current generation")
	status, err := s.Status(ownerCtx, snapshot.RootID, snapshot.NoteID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusCompleted, status.State)
}

func TestVNoteBatchFailurePersistsRecoveryEvenWhenTaskHasRetriesRemaining(t *testing.T) {
	svc, db, knowledge, ctx, snapshot := vnoteFixture(t)
	stage, err := svc.Import(ctx, snapshot)
	require.NoError(t, err)
	var generation types.VNoteImportGeneration
	require.NoError(t, db.First(&generation, "attempt=?", stage.Attempt).Error)
	stageCtx := types.WithTaskRetryMetadata(types.WithVNoteAdmission(ctx, stage.Attempt), 0, 3)
	doc, err := knowledge.repo.GetKnowledgeByID(stageCtx, 7, stage.KnowledgeID)
	require.NoError(t, err)
	admission := knowledge.repo.(interface {
		BeginVNoteProcessing(context.Context, string, string, string) (bool, error)
	})
	claimed, err := admission.BeginVNoteProcessing(stageCtx, doc.ID, generation.ContentHash, generation.Attempt)
	require.NoError(t, err)
	require.True(t, claimed)
	doc.ParseStatus = types.ParseStatusProcessing
	stageCtx, processor, _, _, chunks, engine, _ := chunkIndexWindowFixture(stageCtx, 1)
	processor.repo = knowledge.repo
	engine.failAt = 1
	kb, err := svc.knowledgeBases.GetKnowledgeBaseByID(ctx, stage.KnowledgeBaseID)
	require.NoError(t, err)
	require.NoError(t, processor.processChunks(stageCtx, kb, doc, chunks))
	var current types.Knowledge
	require.NoError(t, db.First(&current, "id=?", doc.ID).Error)
	require.Equal(t, types.ParseStatusProcessing, current.ParseStatus)
	require.Contains(t, current.ErrorMessage, "index write failed")
	require.Len(t, engine.indexSizes, 1)
	// Revocation still permits diagnostics for this attempt, never body/status
	// changes or a released global slot.
	require.NoError(t, svc.ApplyControl(ctx, snapshot.Subject, snapshot.WorkspaceID, 2, false, []string{snapshot.RootID}))
	originalTitle := current.Title
	originalMetadata := append([]byte(nil), current.Metadata...)
	current.ParseStatus = types.ParseStatusFailed
	current.ErrorMessage = "recovery required after revocation"
	current.Title = "must not replace"
	current.Metadata = []byte(`{"invalid":"must not replace"}`)
	require.NoError(t, knowledge.repo.UpdateKnowledge(stageCtx, &current))
	require.NoError(t, db.First(&current, "id=?", doc.ID).Error)
	require.Equal(t, types.ParseStatusProcessing, current.ParseStatus)
	require.Equal(t, originalTitle, current.Title)
	require.JSONEq(t, string(originalMetadata), string(current.Metadata))
	require.Contains(t, current.ErrorMessage, "recovery required")
}

func TestVNoteStableDocumentAndChunkIDsAcrossPublishedGenerations(t *testing.T) {
	s, db, knowledge, ctx, snapshot := vnoteFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	ctx = types.WithExecutionTenant(ctx, 7)
	first, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	finishVNoteGeneration(t, db, ctx, first.KnowledgeID)
	logical := types.VNoteLogicalKnowledgeID(first.KnowledgeBaseID, snapshot.NoteID)
	chunks := repository.NewChunkRepository(db)
	require.NoError(t, chunks.CreateChunks(ctx, []*types.Chunk{{ID: "first-chunk", TenantID: 7, KnowledgeBaseID: first.KnowledgeBaseID, KnowledgeID: first.KnowledgeID, Content: "old", ChunkType: types.ChunkTypeText, IsEnabled: true}}))
	doc, err := knowledge.repo.GetKnowledgeByID(ctx, 7, logical)
	require.NoError(t, err)
	require.Equal(t, first.KnowledgeID, doc.ID)
	encoded, err := json.Marshal(doc)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))
	require.Equal(t, logical, body["id"])
	snapshot.Markdown = "new content"
	second, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	finishVNoteGeneration(t, db, ctx, second.KnowledgeID)
	require.NoError(t, chunks.CreateChunks(ctx, []*types.Chunk{{ID: "second-chunk", TenantID: 7, KnowledgeBaseID: first.KnowledgeBaseID, KnowledgeID: second.KnowledgeID, Content: "new", ChunkType: types.ChunkTypeText, IsEnabled: true}}))
	doc, err = knowledge.repo.GetKnowledgeByID(ctx, 7, logical)
	require.NoError(t, err)
	require.Equal(t, second.KnowledgeID, doc.ID)
	batch, err := knowledge.repo.GetKnowledgeBatch(ctx, 7, []string{logical})
	require.NoError(t, err)
	require.Len(t, batch, 1)
	require.Equal(t, second.KnowledgeID, batch[0].ID)
	batch, err = knowledge.repo.GetKnowledgeBatchByIDOnly(ctx, []string{first.KnowledgeID})
	require.NoError(t, err)
	require.Len(t, batch, 1)
	require.Equal(t, second.KnowledgeID, batch[0].ID)
	encoded, err = json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &body))
	require.Equal(t, logical, body["id"])
	chunkSvc := &chunkService{chunkRepository: chunks, knowledgeRepo: knowledge.repo}
	page, err := chunkSvc.ListPagedChunksByKnowledgeID(ctx, logical, &types.Pagination{Page: 1, PageSize: 20}, []types.ChunkType{types.ChunkTypeText})
	require.NoError(t, err)
	encoded, err = json.Marshal(page)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"knowledge_id":"`+logical+`"`)
	require.Contains(t, string(encoded), `"content":"new"`)
	require.NotContains(t, string(encoded), `"content":"old"`)
	_, err = chunkSvc.GetChunkByIDOnly(ctx, "first-chunk")
	require.ErrorIs(t, err, ErrChunkNotFound)
}

func TestVNoteImportSameSourceIDsRemainPrivatePerUser(t *testing.T) {
	s, db, _, ctx, snapshot := vnoteFixture(t)
	alice, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	finishVNoteGeneration(t, db, ctx, alice.KnowledgeID)
	bobContext := types.WithPrincipal(types.WithCaller(context.Background(), types.Caller{UserID: "bob", TenantID: 7, Role: types.TenantRoleOwner}), types.Principal{Type: types.PrincipalWebUser, ID: "bob"})
	snapshot.Subject = "bob-subject"
	require.NoError(t, s.ApplyControl(bobContext, snapshot.Subject, snapshot.WorkspaceID, snapshot.ConfigVersion, true, []string{"root"}))
	bob, err := s.Import(bobContext, snapshot)
	require.NoError(t, err)
	require.NotEqual(t, alice.KnowledgeBaseID, bob.KnowledgeBaseID)
	require.NotEqual(t, alice.KnowledgeID, bob.KnowledgeID)
	_, err = s.knowledgeBases.GetKnowledgeBaseByID(bobContext, alice.KnowledgeBaseID)
	require.ErrorIs(t, err, repository.ErrKnowledgeBaseNotFound)
}

func TestVNoteStatusIsScopedToBoundOwner(t *testing.T) {
	s, db, _, ctx, snapshot := vnoteFixture(t)
	missing, err := s.Status(ctx, snapshot.RootID, snapshot.NoteID)
	require.NoError(t, err)
	require.Equal(t, "not_imported", missing.State)
	imported, err := s.Import(ctx, snapshot)
	require.NoError(t, err)
	status, err := s.Status(ctx, snapshot.RootID, snapshot.NoteID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, status.State)
	require.Equal(t, imported.KnowledgeID, status.KnowledgeID)
	finishVNoteGeneration(t, db, ctx, imported.KnowledgeID)
	status, err = s.Status(ctx, snapshot.RootID, snapshot.NoteID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusCompleted, status.State)
	other := types.WithPrincipal(types.WithCaller(context.Background(), types.Caller{UserID: "bob", TenantID: 7, Role: types.TenantRoleOwner}), types.Principal{Type: types.PrincipalWebUser, ID: "bob"})
	status, err = s.Status(other, snapshot.RootID, snapshot.NoteID)
	require.NoError(t, err)
	require.Equal(t, "not_imported", status.State)
	require.Empty(t, status.KnowledgeID)
}
