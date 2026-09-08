package router

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestMatchesContractReviewRequiresTaskTypeReviewAndRun(t *testing.T) {
	payload, err := json.Marshal(types.ContractReviewTaskPayload{
		ReviewID:      "review-1",
		AnalysisRunID: "run-1",
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		taskType string
		reviewID string
		runID    string
		want     bool
	}{
		{name: "analysis exact", taskType: types.TypeContractReviewAnalyze, reviewID: "review-1", runID: "run-1", want: true},
		{name: "document exact", taskType: types.TypeContractReviewDocumentProcess, reviewID: "review-1", runID: "run-1", want: true},
		{name: "old run", taskType: types.TypeContractReviewAnalyze, reviewID: "review-1", runID: "run-old", want: false},
		{name: "other review", taskType: types.TypeContractReviewAnalyze, reviewID: "review-2", runID: "run-1", want: false},
		{name: "unrelated task", taskType: types.TypeSummaryGeneration, reviewID: "review-1", runID: "run-1", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, matchesContractReview(test.taskType, payload, test.reviewID, test.runID))
		})
	}
}
