package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type smartArchiveSourceCleanupCatalog struct {
	interfaces.ResourceCatalog
	releaseErr       error
	markDeletedCalls int
}

func (c *smartArchiveSourceCleanupCatalog) Release(context.Context, string, string, string) (int64, error) {
	return 0, c.releaseErr
}

func (c *smartArchiveSourceCleanupCatalog) MarkDeleted(context.Context, string) error {
	c.markDeletedCalls++
	return nil
}

type smartArchiveSourceCleanupFiles struct {
	interfaces.FileService
	deleteErr  error
	deletedRef []string
}

func (f *smartArchiveSourceCleanupFiles) DeleteFile(_ context.Context, reference string) error {
	f.deletedRef = append(f.deletedRef, reference)
	return f.deleteErr
}

func TestSmartArchiveSourceCleanupTreatsMissingResourceAsAlreadyDeleted(t *testing.T) {
	files := &smartArchiveSourceCleanupFiles{}
	service := &smartArchiveService{
		files: files,
		resources: &smartArchiveSourceCleanupCatalog{
			releaseErr: errors.New("resource not found"),
		},
	}

	require.NoError(t, service.deleteArchiveSourceFile(context.Background(), &types.ArchiveDocument{
		ID: "document-1", FilePath: "resource://gone",
	}))
	require.Empty(t, files.deletedRef)
}

func TestSmartArchiveSourceCleanupTreatsMissingPhysicalFileAsAlreadyDeleted(t *testing.T) {
	files := &smartArchiveSourceCleanupFiles{deleteErr: os.ErrNotExist}
	catalog := &smartArchiveSourceCleanupCatalog{}
	service := &smartArchiveService{files: files, resources: catalog}

	require.NoError(t, service.deleteArchiveSourceFile(context.Background(), &types.ArchiveDocument{
		ID: "document-1", FilePath: "resource://gone",
	}))
	require.Equal(t, []string{"resource://gone"}, files.deletedRef)
	require.Equal(t, 1, catalog.markDeletedCalls)
}

type smartArchiveTaskRecorder struct {
	taskIDs   []string
	processAt []time.Time
	errors    []error
}

func (r *smartArchiveTaskRecorder) Enqueue(_ *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	for _, option := range opts {
		if option.Type() == asynq.TaskIDOpt {
			r.taskIDs = append(r.taskIDs, option.Value().(string))
		}
		if option.Type() == asynq.ProcessAtOpt {
			r.processAt = append(r.processAt, option.Value().(time.Time))
		}
	}
	if len(r.errors) > 0 {
		err := r.errors[0]
		r.errors = r.errors[1:]
		return nil, err
	}
	return &asynq.TaskInfo{}, nil
}

type smartArchiveMirrorRetryRepo struct {
	interfaces.ArchiveRepository
	doc *types.ArchiveDocument
}

func (r *smartArchiveMirrorRetryRepo) GetDocument(context.Context, uint64, string) (*types.ArchiveDocument, error) {
	copy := *r.doc
	return &copy, nil
}

func (r *smartArchiveMirrorRetryRepo) RetryMirrorDocument(context.Context, uint64, string) error {
	if r.doc.MirrorStatus != types.ArchiveMirrorFailed {
		return errors.New("invalid mirror state")
	}
	r.doc.MirrorStatus = types.ArchiveMirrorPending
	r.doc.MirrorErrorMessage = ""
	return nil
}

func (r *smartArchiveMirrorRetryRepo) RollbackMirrorRetry(_ context.Context, _ uint64, _ string, message string) error {
	if r.doc.MirrorStatus != types.ArchiveMirrorPending {
		return errors.New("invalid mirror state")
	}
	r.doc.MirrorStatus = types.ArchiveMirrorFailed
	r.doc.MirrorErrorMessage = message
	return nil
}

type smartArchiveMirrorRetryArtifacts struct {
	interfaces.DocumentParseArtifactRepository
}

func (*smartArchiveMirrorRetryArtifacts) GetByFingerprint(context.Context, uint64, string, string) (*types.DocumentParseArtifact, error) {
	return &types.DocumentParseArtifact{ID: "artifact-1"}, nil
}

type smartArchiveMirrorRetryKnowledge struct {
	interfaces.KnowledgeService
}

func TestSmartArchiveRetryUsesFreshTaskIDAfterCancellation(t *testing.T) {
	recorder := &smartArchiveTaskRecorder{}
	service := &smartArchiveService{task: recorder}
	item := &types.ArchiveImportItem{TenantID: 1, ID: "item-1", FileHash: "hash-1", ExtractionVersion: "1.0"}

	require.NoError(t, service.enqueueImportItem(context.Background(), item))
	require.NoError(t, service.enqueueImportItem(context.Background(), item))
	require.Len(t, recorder.taskIDs, 2)
	require.NotEqual(t, recorder.taskIDs[0], recorder.taskIDs[1])
	require.Contains(t, recorder.taskIDs[0], "smart-archive-item-1-")
	require.Contains(t, recorder.taskIDs[1], "smart-archive-item-1-")
}

func TestSmartArchiveMirrorRetryUsesFreshTaskID(t *testing.T) {
	recorder := &smartArchiveTaskRecorder{}
	service := &smartArchiveService{task: recorder}
	doc := &types.ArchiveDocument{TenantID: 1, ID: "document-1"}

	require.NoError(t, service.enqueueMirrorTask(context.Background(), doc))
	require.NoError(t, service.enqueueMirrorTask(context.Background(), doc))
	require.Len(t, recorder.taskIDs, 2)
	require.NotEqual(t, recorder.taskIDs[0], recorder.taskIDs[1])
	require.Contains(t, recorder.taskIDs[0], "smart-archive-mirror-document-1-")
	require.Contains(t, recorder.taskIDs[1], "smart-archive-mirror-document-1-")
}

func TestSmartArchiveMirrorRecoveryWaitsForLiveLease(t *testing.T) {
	recorder := &smartArchiveTaskRecorder{}
	service := &smartArchiveService{task: recorder}
	leaseUntil := time.Now().UTC().Add(time.Minute)
	doc := &types.ArchiveDocument{
		TenantID: 1, ID: "document-1", MirrorStatus: types.ArchiveMirrorProcessing, MirrorLeaseUntil: &leaseUntil,
	}

	require.NoError(t, service.enqueueMirrorTask(context.Background(), doc))
	require.Equal(t, []time.Time{leaseUntil}, recorder.processAt)
}

func TestSmartArchiveMirrorRetryRemainsAvailableAfterEnqueueFailure(t *testing.T) {
	repo := &smartArchiveMirrorRetryRepo{doc: &types.ArchiveDocument{
		ID: "document-1", TenantID: 1, FileHash: "hash-1",
		ExtractionStatus: types.ArchiveExtractionCompleted,
		MirrorStatus:     types.ArchiveMirrorFailed,
	}}
	queue := &smartArchiveTaskRecorder{errors: []error{errors.New("redis unavailable")}}
	service := &smartArchiveService{
		repo: repo, task: queue,
		parseArtifacts: &smartArchiveMirrorRetryArtifacts{},
		knowledge:      &smartArchiveMirrorRetryKnowledge{},
	}

	_, err := service.RetryMirror(context.Background(), 1, "document-1")
	require.EqualError(t, err, "redis unavailable")
	require.Equal(t, types.ArchiveMirrorFailed, repo.doc.MirrorStatus)
	require.Equal(t, "redis unavailable", repo.doc.MirrorErrorMessage)

	result, err := service.RetryMirror(context.Background(), 1, "document-1")
	require.NoError(t, err)
	require.Equal(t, types.ArchiveMirrorPending, result.MirrorStatus)
	require.Len(t, queue.taskIDs, 2)
	require.NotEqual(t, queue.taskIDs[0], queue.taskIDs[1])
}
