package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	contractRepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newContractReviewServiceTest(t *testing.T) (*contractReviewService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.ContractReview{}, &types.ContractReviewClause{}, &types.ContractReviewIssue{}, &types.ResourceBinding{}))
	return &contractReviewService{repo: contractRepo.NewContractReviewRepository(db)}, db
}

type contractReviewPurgeFileStub struct {
	interfaces.FileService
	deleted []string
}

func (s *contractReviewPurgeFileStub) DeleteFile(_ context.Context, reference string) error {
	s.deleted = append(s.deleted, reference)
	return nil
}

type contractReviewPurgeCatalogStub struct {
	interfaces.ResourceCatalog
	remaining map[string]int64
}

func (s *contractReviewPurgeCatalogStub) Release(_ context.Context, reference, _ string, _ string) (int64, error) {
	if count, ok := s.remaining[reference]; ok {
		return count, nil
	}
	return 0, nil
}

func TestDeleteTenantDataRemovesChildrenAndCleansUnsharedStorage(t *testing.T) {
	svc, db := newContractReviewServiceTest(t)
	ctx := context.Background()
	owned := &types.ContractReview{TenantID: 1, UserID: "u1", ResourceRef: "local://contract-1"}
	shared := &types.ContractReview{TenantID: 1, UserID: "u2", ResourceRef: "local://contract-shared"}
	otherTenant := &types.ContractReview{TenantID: 2, UserID: "u3", ResourceRef: "local://other"}
	for _, review := range []*types.ContractReview{owned, shared, otherTenant} {
		require.NoError(t, svc.repo.Create(ctx, review))
	}
	clause := &types.ContractReviewClause{ReviewID: owned.ID, Sequence: 0, Title: "Payment"}
	require.NoError(t, svc.repo.ReplaceClauses(ctx, owned.ID, []*types.ContractReviewClause{clause}))
	issue := &types.ContractReviewIssue{
		ReviewID: owned.ID, ClauseID: clause.ID, Fingerprint: "purge-fingerprint",
		RiskLevel: types.ContractReviewRiskHigh, Title: "risk", Explanation: "explanation",
		OriginalQuote: "quote", Suggestion: "suggestion",
	}
	require.NoError(t, svc.repo.UpsertIssue(ctx, issue))
	require.NoError(t, db.Create(&types.ResourceBinding{
		ResourceID: "resource-1", TenantID: 1, OwnerType: types.ResourceOwnerContractReview,
		OwnerID: owned.ID, Relation: types.ResourceRelationSourceFile,
	}).Error)

	files := &contractReviewPurgeFileStub{}
	svc.files = files
	svc.resources = &contractReviewPurgeCatalogStub{remaining: map[string]int64{
		"local://contract-1":      0,
		"local://contract-shared": 1,
	}}

	require.NoError(t, svc.DeleteTenantData(ctx, 1))
	require.ElementsMatch(t, []string{"local://contract-1"}, files.deleted)
	var count int64
	require.NoError(t, db.Unscoped().Model(&types.ContractReview{}).Where("tenant_id = ?", 1).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.ContractReviewClause{}).Where("review_id = ?", owned.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.ContractReviewIssue{}).Where("review_id = ?", owned.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.ResourceBinding{}).Where("owner_id = ?", owned.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Unscoped().Model(&types.ContractReview{}).Where("tenant_id = ?", 2).Count(&count).Error)
	require.EqualValues(t, 1, count)

	// A second purge is a no-op and must not repeat physical deletion.
	require.NoError(t, svc.DeleteTenantData(ctx, 1))
	require.ElementsMatch(t, []string{"local://contract-1"}, files.deleted)
}

type contractReviewUploadFileStub struct {
	interfaces.FileService
	saved []byte
}

func (s *contractReviewUploadFileStub) SaveBytes(_ context.Context, data []byte, _ uint64, fileName string, _ bool) (string, error) {
	s.saved = append([]byte(nil), data...)
	return "local://" + fileName, nil
}

type contractReviewUploadQueueStub struct {
	interfaces.TaskEnqueuer
	calls int
	err   error
}

func (s *contractReviewUploadQueueStub) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &asynq.TaskInfo{ID: task.Type()}, nil
}

func TestContractReviewBulkActionValidatesInputBeforeRepositoryAccess(t *testing.T) {
	svc := &contractReviewService{}
	if _, err := svc.BulkAction(context.Background(), 1, "u1", nil, types.ContractReviewBulkArchive); err == nil {
		t.Fatal("empty bulk selection must be rejected")
	}
	ids := make([]string, 501)
	for index := range ids {
		ids[index] = "review"
	}
	if _, err := svc.BulkAction(context.Background(), 1, "u1", ids, types.ContractReviewBulkDelete); err == nil {
		t.Fatal("bulk selections over 500 items must be rejected")
	}
}

func TestBuildReviewClausesKeepsDocumentOrderAndOffsets(t *testing.T) {
	content := "# 付款条款\n合同价款应在交付后 30 日内支付。\n\n# Liability\nLiability is unlimited."
	clauses := buildReviewClauses("review-1", content)
	if len(clauses) == 0 {
		t.Fatal("expected at least one clause")
	}
	for index, clause := range clauses {
		if clause.Sequence != index {
			t.Fatalf("sequence=%d, want %d", clause.Sequence, index)
		}
		if clause.SourceStart < 0 || clause.SourceEnd <= clause.SourceStart || clause.SourceEnd > len([]rune(content)) {
			t.Fatalf("invalid source range: %+v", clause)
		}
	}
}

func TestBuildReviewClausesLabelsGeneratedWindowsAsAnalysisSegments(t *testing.T) {
	content := strings.Repeat("合同内容用于测试分析窗口。\n", 400)
	clauses := buildReviewClauses("review-1", content)
	if len(clauses) < 2 {
		t.Fatalf("expected multiple analysis windows, got %d", len(clauses))
	}
	if clauses[0].Title != "Analysis segment 1" {
		t.Fatalf("generated window title=%q, want %q", clauses[0].Title, "Analysis segment 1")
	}
}

func TestReviewDocumentContextIncludesTextOutsidePrimaryWindow(t *testing.T) {
	primary := "二、合同标的\n视频彩铃服务。"
	other := "\n三、价格形式及合同价款\n3.2合同价款包含范围：视频彩铃服务计入合同总价。"
	document := primary + other
	clause := &types.ContractReviewClause{SourceStart: 0, SourceEnd: len([]rune(primary))}

	context := newReviewDocumentContext(document).CrossWindowContext(clause)
	if !strings.Contains(context, "合同价款包含范围") || !strings.Contains(context, "视频彩铃服务计入合同总价") {
		t.Fatalf("cross-window context omitted the related provision: %q", context)
	}
}

func TestReviewDocumentContextRetrievesRelatedProvisionFromLongDocument(t *testing.T) {
	primary := "二、合同标的\n视频彩铃服务需要审查。\n"
	filler := strings.Repeat("普通背景文本，不包含当前服务主题。\n", 700)
	other := "3.2合同价款包含范围\n视频彩铃服务已经包含在固定总价中。\n"
	document := primary + filler + other
	clause := &types.ContractReviewClause{SourceStart: 0, SourceEnd: len([]rune(primary))}

	context := newReviewDocumentContext(document).CrossWindowContext(clause)
	if !strings.Contains(context, "合同价款包含范围") || !strings.Contains(context, "视频彩铃服务已经包含在固定总价中") {
		t.Fatalf("long-document context omitted the related provision: %q", context)
	}
	if got := len([]rune(context)); got > contractReviewRelatedContextMaxRunes {
		t.Fatalf("cross-window context length=%d, want <=%d", got, contractReviewRelatedContextMaxRunes)
	}
}

func TestParseReviewModelJSONRejectsFencedJSONObject(t *testing.T) {
	var output reviewBatchOutput
	err := parseModelJSON("```json\n{\"issues\":[{\"risk_level\":\"high\",\"title\":\"Payment\",\"explanation\":\"Early payment\",\"original_quote\":\"pay now\",\"suggestion\":\"pay after delivery\"}]}\n```", &output)
	if err == nil {
		t.Fatalf("markdown-fenced output must be rejected: %#v", output)
	}
}

func TestContractReviewRiskNormalization(t *testing.T) {
	if validRisk("HIGH") != types.ContractReviewRiskHigh {
		t.Fatal("HIGH should normalize to high")
	}
	if validRisk("unknown") != "" {
		t.Fatal("unknown risk must remain invalid instead of defaulting to medium")
	}
}

func TestIssueFingerprintIsIdempotent(t *testing.T) {
	a := issueFingerprint("review", "clause", " Payment risk ", "pay  now")
	b := issueFingerprint("review", "clause", "payment risk", "pay now")
	if a != b {
		t.Fatalf("fingerprint changed across harmless whitespace: %s != %s", a, b)
	}
}

func TestFindReviewQuoteRangeIgnoresFormattingWhitespace(t *testing.T) {
	body := "视频\n 彩铃服务，费用按发送成功人数结算。"
	quote := "视频彩铃服务，费用按发送成功人数结算。"
	start, end, ok := findReviewQuoteRange(body, quote)
	if !ok {
		t.Fatal("quote with formatting whitespace should be locatable")
	}
	if got := string([]rune(body)[start:end]); got != body {
		t.Fatalf("source range = %q, want %q", got, body)
	}
}

func TestReviewIssuesDeduplicateOverlappingEvidence(t *testing.T) {
	first := reviewIssueOutput{
		Title:         "5G消息发送量与费用结算机制不明确",
		OriginalQuote: "计划发送20万条，通信费和数据服务费等费用按发送成功人数据实结算。",
	}
	duplicate := reviewIssueOutput{
		Title:         "数据服务费用结算依据模糊",
		OriginalQuote: "通信费和数据服务费等费用按发送成功人数据实结算。",
	}
	if !reviewIssuesAreDuplicate(first, duplicate) {
		t.Fatal("overlapping evidence with the same topic should be deduplicated")
	}
}

func TestContractReviewPromptsRequireChineseAnalysis(t *testing.T) {
	for name, prompt := range map[string]string{"clause": contractReviewClauseSystemPrompt, "overview": contractReviewOverviewSystemPrompt} {
		if !strings.Contains(prompt, "Simplified Chinese") {
			t.Fatalf("%s prompt must require Simplified Chinese output", name)
		}
	}
	if !strings.Contains(contractReviewClauseSystemPrompt, "original language and wording") {
		t.Fatal("clause prompt must preserve the original quotation for document positioning")
	}
	if !strings.Contains(contractReviewClauseSystemPrompt, "exactly one JSON object") || !strings.Contains(contractReviewClauseSystemPrompt, "at most five issues") {
		t.Fatal("clause prompt must enforce a compact machine-readable response")
	}
	for _, required := range []string{"explicit provision covers", "evidence_refs", "facts are extracted by the service", "Use only evidence_id values", "exact passage"} {
		if !strings.Contains(contractReviewClauseSystemPrompt, required) {
			t.Fatalf("clause prompt must include generic evidence rule %q", required)
		}
	}
	for _, forbidden := range []string{"视频彩铃", "5G消息", "合同价款包含范围"} {
		if strings.Contains(contractReviewClauseSystemPrompt, forbidden) {
			t.Fatalf("clause prompt must not contain contract-specific rule %q", forbidden)
		}
	}
	if !strings.Contains(contractReviewClauseSystemPrompt, "cross-window context") || !strings.Contains(contractReviewClauseSystemPrompt, "primary window") {
		t.Fatal("clause prompt must distinguish primary and cross-window context")
	}
}

func TestContractReviewClauseRetryUsesBoundedCompletionBudget(t *testing.T) {
	for current, want := range map[int]int{0: 8192, 1800: 8192, 4096: 8192, 8192: 8192, 12000: 12000} {
		if got := contractReviewClauseRetryTokens(current); got != want {
			t.Fatalf("retry tokens for %d = %d, want %d", current, got, want)
		}
	}
}

func TestContractReviewOutputReachedLimit(t *testing.T) {
	for _, reason := range []string{"length", "max_tokens", "max_completion_tokens"} {
		if !contractReviewOutputReachedLimit(&types.ChatResponse{FinishReason: reason}) {
			t.Fatalf("finish reason %q should be treated as truncated", reason)
		}
	}
	if contractReviewOutputReachedLimit(&types.ChatResponse{FinishReason: "stop"}) {
		t.Fatal("stop should not be treated as truncated")
	}
}

func TestContractReviewUploadRejectsUnsupportedAndMalformedFiles(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	review, err := svc.Create(context.Background(), 7, "u1")
	require.NoError(t, err)

	_, err = svc.Upload(context.Background(), 7, "u1", review.ID, "contract.txt", "text/plain", 3, bytes.NewReader([]byte("txt")))
	require.ErrorIs(t, err, ErrContractReviewInvalidFile)

	_, err = svc.Upload(context.Background(), 7, "u1", review.ID, "contract.pdf", "application/pdf", 3, bytes.NewReader([]byte("not-pdf")))
	require.ErrorIs(t, err, ErrContractReviewInvalidFile)
}

func TestContractReviewUploadAcceptsPDFAndDOCX(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	files := &contractReviewUploadFileStub{}
	queue := &contractReviewUploadQueueStub{}
	svc.files = files
	svc.tasks = queue

	tests := []struct {
		name     string
		fileName string
		mimeType string
		data     []byte
	}{
		{name: "pdf", fileName: "contract.pdf", mimeType: "application/pdf", data: []byte("%PDF-1.7 test")},
		{name: "docx", fileName: "contract.docx", mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", data: []byte{'P', 'K', 3, 4, 'x'}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			review, err := svc.Create(context.Background(), 7, "u1")
			require.NoError(t, err)
			stored, err := svc.Upload(context.Background(), 7, "u1", review.ID, test.fileName, test.mimeType, int64(len(test.data)), bytes.NewReader(test.data))
			require.NoError(t, err)
			require.Equal(t, types.ContractReviewStatusUploading, stored.Status)
			require.Equal(t, test.data, files.saved)
		})
	}
	require.Equal(t, 2, queue.calls)
}

func TestContractReviewStartAndRetryMarkEnqueueFailures(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	queue := &contractReviewUploadQueueStub{err: errors.New("queue unavailable")}
	svc.tasks = queue

	review, err := svc.Create(context.Background(), 7, "u1")
	require.NoError(t, err)
	review.Status = types.ContractReviewStatusReady
	require.NoError(t, svc.repo.Update(context.Background(), review))
	started, err := svc.Start(context.Background(), 7, "u1", review.ID)
	require.ErrorContains(t, err, "queue unavailable")
	require.Equal(t, types.ContractReviewStatusFailed, started.Status)

	started.Status = types.ContractReviewStatusCompleted
	started.ExtractedContent = "contract text"
	require.NoError(t, svc.repo.Update(context.Background(), started))
	retried, err := svc.Retry(context.Background(), 7, "u1", review.ID)
	require.ErrorContains(t, err, "queue unavailable")
	require.Equal(t, types.ContractReviewStatusFailed, retried.Status)
	require.Equal(t, 2, queue.calls)
}

func TestContractReviewRejectsMutationsWhileAnalysisIsRunning(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	review, err := svc.Create(context.Background(), 7, "u1")
	require.NoError(t, err)
	review.Status = types.ContractReviewStatusAnalyzing
	require.NoError(t, svc.repo.Update(context.Background(), review))

	archived := true
	_, err = svc.Update(context.Background(), 7, "u1", review.ID, "", "", "", nil, &archived)
	require.ErrorIs(t, err, ErrContractReviewInvalidState)
	_, err = svc.Update(context.Background(), 7, "u1", review.ID, "", "general-contract-review", "customer", nil, nil)
	require.ErrorIs(t, err, ErrContractReviewInvalidState)
	_, err = svc.Start(context.Background(), 7, "u1", review.ID)
	require.ErrorIs(t, err, ErrContractReviewInvalidState)
}

func TestContractReviewBulkActionDeduplicatesIDsAndReportsPartialFailures(t *testing.T) {
	svc, _ := newContractReviewServiceTest(t)
	review, err := svc.Create(context.Background(), 7, "u1")
	require.NoError(t, err)

	result, err := svc.BulkAction(context.Background(), 7, "u1", []string{" ", review.ID, review.ID, "missing"}, types.ContractReviewBulkArchive)
	require.NoError(t, err)
	require.Equal(t, 2, result.Requested)
	require.Equal(t, 1, result.Succeeded)
	require.Equal(t, 1, result.Failed)
	stored, err := svc.repo.Get(context.Background(), 7, "u1", review.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.ArchivedAt)
}
