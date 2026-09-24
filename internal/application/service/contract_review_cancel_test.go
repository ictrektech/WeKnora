package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type contractReviewTaskInspectorStub struct {
	deleted      int
	cancelled    int
	cancelCalls  int
	queued       bool
	queuedErr    error
	lastReviewID string
	lastAnalysis string
}

func (s *contractReviewTaskInspectorStub) CancelTasksForContractReview(_ context.Context, reviewID, analysisRunID string) (int, int, error) {
	s.cancelCalls++
	s.lastReviewID = reviewID
	s.lastAnalysis = analysisRunID
	return s.deleted, s.cancelled, nil
}

func (s *contractReviewTaskInspectorStub) HasQueuedTasksForContractReview(_ context.Context, _ string, _ string) (bool, error) {
	return s.queued, s.queuedErr
}

var _ interfaces.ContractReviewTaskInspector = (*contractReviewTaskInspectorStub)(nil)

func liveContractReviewForTest(t *testing.T, svc *contractReviewService) *types.ContractReview {
	t.Helper()
	review, err := svc.Create(context.Background(), 7, "u1")
	require.NoError(t, err)
	review.Status = types.ContractReviewStatusAnalyzing
	review.AnalysisRunID = "run-cancel-test"
	review.ConfigHash = "config-cancel-test"
	now := time.Now()
	review.StartedAt = &now
	require.NoError(t, svc.repo.Update(context.Background(), review))
	return review
}

func TestContractReviewCancelMarksRunTerminalAndStopsMatchingQueueTasks(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	inspector := &contractReviewTaskInspectorStub{deleted: 2, cancelled: 1}
	svc.inspector = inspector
	review := liveContractReviewForTest(t, svc)

	cancelled, err := svc.Cancel(context.Background(), review.TenantID, review.UserID, review.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContractReviewStatusCancelled, cancelled.Status)
	require.Equal(t, "合同审查已取消，可重试。", cancelled.ErrorMessage)
	require.Equal(t, 1, inspector.cancelCalls)
	require.Equal(t, review.ID, inspector.lastReviewID)
	require.Equal(t, review.AnalysisRunID, inspector.lastAnalysis)

	stored, err := svc.repo.Get(context.Background(), review.TenantID, review.UserID, review.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContractReviewStatusCancelled, stored.Status)

	// A worker that was already holding the old in-memory row must not be able
	// to turn the cancelled run into a late failure.
	require.NoError(t, svc.fail(context.Background(), review, errors.New("late worker error")))
	stored, err = svc.repo.Get(context.Background(), review.TenantID, review.UserID, review.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContractReviewStatusCancelled, stored.Status)
}

func TestContractReviewFailPersistsAfterWorkerContextCancellation(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	review := liveContractReviewForTest(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.fail(ctx, review, context.DeadlineExceeded)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	stored, getErr := svc.repo.Get(context.Background(), review.TenantID, review.UserID, review.ID)
	require.NoError(t, getErr)
	require.Equal(t, types.ContractReviewStatusFailed, stored.Status)
	require.Equal(t, "合同审查任务超时，可重试。", stored.ErrorMessage)
}

type contractReviewHousekeepingInspector struct {
	fakeTaskInspector
	queued bool
	err    error
}

func (s contractReviewHousekeepingInspector) HasQueuedTasksForContractReview(_ context.Context, _ string, _ string) (bool, error) {
	return s.queued, s.err
}

func (contractReviewHousekeepingInspector) CancelTasksForContractReview(_ context.Context, _ string, _ string) (int, int, error) {
	return 0, 0, nil
}

var _ interfaces.ContractReviewTaskInspector = contractReviewHousekeepingInspector{}

func TestHousekeepingRecoversOrphanedContractReview(t *testing.T) {
	svc, db := newContractReviewServiceTest(t)
	review := liveContractReviewForTest(t, svc)
	stale := time.Now().Add(-(contractReviewStaleThreshold() + time.Minute))
	require.NoError(t, db.Model(&types.ContractReview{}).Where("id = ?", review.ID).Updates(map[string]interface{}{
		"started_at": stale,
		"updated_at": stale,
	}).Error)

	housekeeping := NewHousekeepingService(db, nil, contractReviewHousekeepingInspector{}, nil)
	housekeeping.sweepContractReviews(context.Background())

	stored, err := svc.repo.Get(context.Background(), review.TenantID, review.UserID, review.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContractReviewStatusFailed, stored.Status)
	require.Contains(t, stored.ErrorMessage, "后台巡检")
}

func TestHousekeepingPreservesQueuedContractReview(t *testing.T) {
	svc, db := newContractReviewServiceTest(t)
	review := liveContractReviewForTest(t, svc)
	stale := time.Now().Add(-(contractReviewStaleThreshold() + time.Minute))
	require.NoError(t, db.Model(&types.ContractReview{}).Where("id = ?", review.ID).Updates(map[string]interface{}{
		"started_at": stale,
		"updated_at": stale,
	}).Error)

	housekeeping := NewHousekeepingService(db, nil, contractReviewHousekeepingInspector{queued: true}, nil)
	housekeeping.sweepContractReviews(context.Background())

	stored, err := svc.repo.Get(context.Background(), review.TenantID, review.UserID, review.ID)
	require.NoError(t, err)
	require.Equal(t, types.ContractReviewStatusAnalyzing, stored.Status)
}
