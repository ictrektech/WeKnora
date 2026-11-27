package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeComputeTextCaller struct {
	handlers map[string]func(attempt int) (*proto.TextInferenceResponse, error)
	counts   map[string]int
}

func (f *fakeComputeTextCaller) GenerateText(_ context.Context, origin string, _ *proto.TextInferenceRequest) (*proto.TextInferenceResponse, error) {
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[origin]++
	return f.handlers[origin](f.counts[origin])
}

type stubLocalChat struct {
	name  string
	calls int
	reply string
	err   error
}

func (s *stubLocalChat) Chat(_ context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &types.ChatResponse{Content: s.reply, FinishReason: "stop"}, nil
}

func (s *stubLocalChat) ChatStream(_ context.Context, _ []chat.Message, _ *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubLocalChat) GetModelName() string { return s.name }
func (s *stubLocalChat) GetModelID() string   { return "local-id" }

type stubLocalModelSvc struct {
	interfaces.ModelService
	chat chat.Chat
}

func (s *stubLocalModelSvc) GetChatModel(_ context.Context, _ string) (chat.Chat, error) {
	return s.chat, nil
}

func fastTestRetryPolicy(maxRetries int, sleeps *[]time.Duration) ComputeTextRetryPolicy {
	return ComputeTextRetryPolicy{
		MaxRetries:  maxRetries,
		BaseBackoff: 100 * time.Millisecond,
		MaxBackoff:  400 * time.Millisecond,
		Sleep: func(_ context.Context, d time.Duration) error {
			*sleeps = append(*sleeps, d)
			return nil
		},
	}
}

func TestExecuteComputeTextInferenceBusyRetryRecovers(t *testing.T) {
	const tc35 = "https://192.168.1.35:1180"
	caller := &fakeComputeTextCaller{
		handlers: map[string]func(int) (*proto.TextInferenceResponse, error){
			tc35: func(attempt int) (*proto.TextInferenceResponse, error) {
				if attempt <= 2 {
					return nil, &docparser.ComputeBusyError{Code: docparser.ComputeErrorCodeBusy}
				}
				return &proto.TextInferenceResponse{
					Content:    "recovered answer",
					DurationMs: 210,
					ModelName:  "qwen3.5:9b",
				}, nil
			},
		},
	}
	localChat := &stubLocalChat{name: "local-qwen", reply: "local answer"}
	var sleeps []time.Duration
	route := &types.ComputeTextRouteConfig{
		Targets:         []types.ComputeTextTarget{{NodeOrigin: tc35, ModelID: "m1"}},
		FallbackToLocal: false,
	}

	res, err := ExecuteComputeTextInference(
		t.Context(), caller, &stubLocalModelSvc{chat: localChat}, route, "local-id",
		[]chat.Message{{Role: "user", Content: "hi"}}, nil, "question_generation",
		fastTestRetryPolicy(4, &sleeps),
	)
	require.NoError(t, err)
	require.Equal(t, tc35, res.Node)
	require.Equal(t, "recovered answer", res.Response.Content)
	require.Equal(t, 2, res.BusyRetries)
	require.Equal(t, int64(210), res.RemoteDurationMs)
	require.Equal(t, []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}, sleeps)
	require.Zero(t, localChat.calls, "must not touch local model when remote recovers")
}

func TestExecuteComputeTextInferenceNoFallbackWhenUnconfigured(t *testing.T) {
	const tc35 = "https://192.168.1.35:1180"
	caller := &fakeComputeTextCaller{
		handlers: map[string]func(int) (*proto.TextInferenceResponse, error){
			tc35: func(int) (*proto.TextInferenceResponse, error) {
				return nil, &docparser.ComputeBusyError{Code: docparser.ComputeErrorCodeBusy}
			},
		},
	}
	localChat := &stubLocalChat{name: "local-qwen", reply: "local answer"}
	var sleeps []time.Duration
	route := &types.ComputeTextRouteConfig{
		Targets:         []types.ComputeTextTarget{{NodeOrigin: tc35, ModelID: "m1"}},
		FallbackToLocal: false,
	}

	res, err := ExecuteComputeTextInference(
		t.Context(), caller, &stubLocalModelSvc{chat: localChat}, route, "local-id",
		[]chat.Message{{Role: "user", Content: "hi"}}, nil, "document_summary",
		fastTestRetryPolicy(2, &sleeps),
	)
	require.Nil(t, res)
	require.Error(t, err)
	require.Zero(t, localChat.calls, "must NOT fall back to local when FallbackToLocal is false")
	require.Equal(t, 3, caller.counts[tc35]) // 1 initial + 2 retries
	var execErr *ComputeTextExecutionError
	require.ErrorAs(t, err, &execErr)
	require.Equal(t, tc35, execErr.Node)
	require.Equal(t, docparser.ComputeErrorCodeBusy, execErr.Reason)
	require.Equal(t, 2, execErr.BusyRetries)
}

func TestExecuteComputeTextInferenceSwitchesOnlyWhenFallbackConfigured(t *testing.T) {
	const tc35 = "https://192.168.1.35:1180"
	const tc36 = "https://192.168.1.36:1180"
	caller := &fakeComputeTextCaller{
		handlers: map[string]func(int) (*proto.TextInferenceResponse, error){
			tc35: func(int) (*proto.TextInferenceResponse, error) {
				return nil, status.Error(codes.Unavailable, "tc35 offline")
			},
			tc36: func(int) (*proto.TextInferenceResponse, error) {
				return nil, &api.HTTPError{StatusCode: http.StatusTooManyRequests, Body: `{"code":"WAIT_COMPUTE_RESOURCE"}`}
			},
		},
	}
	localChat := &stubLocalChat{name: "local-qwen", reply: "local fallback answer"}
	var sleeps []time.Duration
	route := &types.ComputeTextRouteConfig{
		Targets: []types.ComputeTextTarget{
			{NodeOrigin: tc35, ModelID: "m1"},
			{NodeOrigin: tc36, ModelID: "m2"},
		},
		FallbackToLocal: true,
	}

	res, err := ExecuteComputeTextInference(
		t.Context(), caller, &stubLocalModelSvc{chat: localChat}, route, "local-id",
		[]chat.Message{{Role: "user", Content: "hi"}}, nil, "document_summary",
		fastTestRetryPolicy(1, &sleeps),
	)
	require.NoError(t, err)
	require.Equal(t, docparser.ComputeNodeLocal, res.Node)
	require.Equal(t, "local fallback answer", res.Response.Content)
	require.Equal(t, 1, localChat.calls)
	require.Len(t, res.Fallbacks, 2)
	require.Equal(t, tc35, res.Fallbacks[0].Node)
	require.Equal(t, "NODE_UNAVAILABLE", res.Fallbacks[0].Reason)
	require.Equal(t, tc36, res.Fallbacks[1].Node)
	require.Equal(t, docparser.ComputeErrorCodeBusy, res.Fallbacks[1].Reason)
}

func TestQuestionDeduplicationAndDeterministicIDs(t *testing.T) {
	qs1 := buildDeterministicGeneratedQuestions(3, []string{"Q1?", "Q2?"})
	qs2 := buildDeterministicGeneratedQuestions(3, []string{"Q1?", "Q2?"})
	require.Equal(t, qs1, qs2)
	require.Equal(t, "q-r3-0", qs1[0].ID)
	require.Equal(t, "q-r3-1", qs1[1].ID)

	meta := &types.DocumentChunkMetadata{
		GeneratedQuestions:         qs1,
		GeneratedQuestionsRevision: 3,
		GeneratedQuestionsAttempt:  2,
		GeneratedQuestionsIndexed:  true,
	}
	require.True(t, isQuestionChunkAlreadyCommitted(meta, 2, 3))
	require.False(t, isQuestionChunkAlreadyCommitted(meta, 3, 3), "new parse attempt must regenerate")
	require.False(t, isQuestionChunkAlreadyCommitted(meta, 2, 4), "edited chunk revision must regenerate")
	meta.GeneratedQuestionsIndexed = false
	require.False(t, isQuestionChunkAlreadyCommitted(meta, 2, 3), "unindexed metadata must retry")
}

type recordedSpanRow struct {
	name       string
	durationMs int64
	output     types.JSONMap
}

type fakeTimedSpanTracker struct {
	noopSpanTracker
	stage      *Span
	subSpans   []recordedSpanRow
	stageEndMs int64
	stageOut   types.JSONMap
}

func (f *fakeTimedSpanTracker) BeginStage(_ context.Context, kid string, attempt int, stage string, _ types.JSONMap) *Span {
	f.stage = &Span{KnowledgeID: kid, Attempt: attempt, SpanID: "mm-stage", Name: stage, Kind: types.SpanKindStage}
	return f.stage
}

func (f *fakeTimedSpanTracker) LookupStage(_ context.Context, _ string, _ int, _ string) *Span {
	return f.stage
}

func (f *fakeTimedSpanTracker) RecordCompletedSubSpan(
	_ context.Context, _ *Span, name, _ string, _, output types.JSONMap, _ time.Time, durationMs int64,
) *Span {
	f.subSpans = append(f.subSpans, recordedSpanRow{name: name, durationMs: durationMs, output: output})
	return &Span{Name: name}
}

func (f *fakeTimedSpanTracker) EndSpanWithTiming(
	_ context.Context, _ *Span, output types.JSONMap, _ time.Time, durationMs int64,
) {
	f.stageEndMs = durationMs
	f.stageOut = output
}

func TestRecordDocumentComputeRecognitionUsesExecutorDurations(t *testing.T) {
	tracker := &fakeTimedSpanTracker{}
	svc := &knowledgeService{spanTracker: tracker}
	ctx := withAttempt(t.Context(), 1)
	images := []docparser.StoredImage{{
		Analysis: &types.DocumentImageAnalysis{
			OCRText:               "page text",
			Description:           "page caption",
			OCRPerformed:          true,
			OCRDurationMs:         2450,
			DescriptionDurationMs: 1320,
		},
	}}
	svc.recordDocumentComputeRecognition(ctx, "kid-1", images, map[string]string{
		"docreader_node":     "https://192.168.1.35:1180",
		"docreader_strategy": "order",
	})
	require.Len(t, tracker.subSpans, 2)
	require.Equal(t, "multimodal.image[0].ocr", tracker.subSpans[0].name)
	require.Equal(t, int64(2450), tracker.subSpans[0].durationMs)
	require.Equal(t, "https://192.168.1.35:1180", tracker.subSpans[0].output["compute_node"])
	require.Equal(t, "multimodal.image[0].caption", tracker.subSpans[1].name)
	require.Equal(t, int64(1320), tracker.subSpans[1].durationMs)
	require.Equal(t, "https://192.168.1.35:1180", tracker.subSpans[1].output["compute_node"])
	require.Equal(t, int64(3770), tracker.stageEndMs)
	require.Equal(t, "https://192.168.1.35:1180", tracker.stageOut["compute_node"])
}

type fakeDocReaderWithText struct {
	interfaces.DocumentReader
	caller computeTextCaller
}

func (f *fakeDocReaderWithText) GenerateText(ctx context.Context, origin string, req *proto.TextInferenceRequest) (*proto.TextInferenceResponse, error) {
	return f.caller.GenerateText(ctx, origin, req)
}

func TestQuestionBatchRetryDeduplicatesCommittedChunks(t *testing.T) {
	const tc35 = "https://192.168.1.35:1180"
	t.Setenv(computeBusyMaxRetriesEnv, "1")
	t.Setenv(computeBusyBaseBackoffMsEnv, "1")
	t.Setenv(computeBusyMaxBackoffMsEnv, "2")

	f := newDocumentWriteFixture(t)
	require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id = ?", "doc").Updates(map[string]any{
		"parse_status": types.ParseStatusFinalizing, "pending_subtasks_count": 1,
	}).Error)
	require.NoError(t, f.db.Create(&types.Chunk{
		ID: "second", TenantID: 7, KnowledgeID: "doc", KnowledgeBaseID: "kb",
		ChunkType: types.ChunkTypeText, Content: "second document body",
	}).Error)

	// Configure tc35 compute route on the KB without local fallback.
	f.kbs.values["kb"].QuestionGenerationConfig = &types.QuestionGenerationConfig{
		Enabled:       true,
		QuestionCount: 2,
		ComputeRoute: &types.ComputeTextRouteConfig{
			Targets:         []types.ComputeTextTarget{{NodeOrigin: tc35, ModelID: "remote-qwen"}},
			FallbackToLocal: false,
		},
	}

	callNum := 0
	caller := &fakeComputeTextCaller{
		handlers: map[string]func(int) (*proto.TextInferenceResponse, error){
			tc35: func(_ int) (*proto.TextInferenceResponse, error) {
				callNum++
				switch callNum {
				case 1:
					return &proto.TextInferenceResponse{Content: "第一个分块问题？", DurationMs: 120, ModelName: "qwen3.5:9b"}, nil
				case 2, 3:
					// Chunk 2 hits busy (initial + 1 retry) -> exhausts retries and triggers Asynq retry.
					return nil, &docparser.ComputeBusyError{Code: docparser.ComputeErrorCodeBusy}
				default:
					return &proto.TextInferenceResponse{Content: "第二个分块问题？", DurationMs: 140, ModelName: "qwen3.5:9b"}, nil
				}
			},
		},
	}
	index := &tableSummaryIndex{}
	f.svc.tenantRepo = questionTenantRepository{TenantRepository: f.svc.tenantRepo}
	f.svc.config = questionTestConfig()
	f.svc.modelService = tableSummaryModelService{model: &stubLocalChat{name: "local-unused", reply: "unused"}}
	f.svc.retrieveEngine = tableSummaryRegistry{index: index}
	f.svc.documentReader = &fakeDocReaderWithText{caller: caller}

	payload, err := json.Marshal(types.QuestionGenerationPayload{
		TenantID: 7, KnowledgeID: "doc", KnowledgeBaseID: "kb",
		ChunkIDs: []string{"chunk", "second"}, QuestionCount: 2, Attempt: 1,
	})
	require.NoError(t, err)

	// Attempt 0: chunk 1 commits, chunk 2 fails with WAIT_COMPUTE_RESOURCE -> returns error for Asynq retry.
	err = f.svc.ProcessQuestionGeneration(types.WithTaskRetryMetadata(f.ctx, 0, 3), asynq.NewTask(types.TypeQuestionGeneration, payload))
	require.Error(t, err)
	require.Len(t, index.indexed, 1, "chunk 1 question should be indexed once")

	// Attempt 1 (retry): chunk 1 must be deduplicated (skipped), chunk 2 succeeds -> total indexed = 2 (no duplicate).
	err = f.svc.ProcessQuestionGeneration(types.WithTaskRetryMetadata(f.ctx, 1, 3), asynq.NewTask(types.TypeQuestionGeneration, payload))
	require.NoError(t, err)
	require.Len(t, index.indexed, 2, "chunk 1 must not be re-indexed on retry")
	require.Equal(t, 4, callNum, "chunk 1 must not trigger another remote LLM call on retry")
}

func TestSummaryRetryUpdatesExistingChunkWithoutDuplicates(t *testing.T) {
	const tc35 = "https://192.168.1.35:1180"
	f := newDocumentWriteFixture(t)
	require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id = ?", "doc").Updates(map[string]any{
		"parse_status": types.ParseStatusFinalizing, "pending_subtasks_count": 1,
	}).Error)
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("id = ?", "chunk").Update("content",
		"这是一段足够长的文档正文内容，用于验证跨节点摘要推理和重试时的原地去重更新逻辑。",
	).Error)
	f.kbs.values["kb"].Type = types.KnowledgeBaseTypeDocument
	f.kbs.values["kb"].EmbeddingModelID = "emb-1"
	f.kbs.values["kb"].IndexingStrategy = types.IndexingStrategy{VectorEnabled: true}
	f.kbs.values["kb"].SummaryComputeConfig = &types.ComputeTextRouteConfig{
		Targets:         []types.ComputeTextTarget{{NodeOrigin: tc35, ModelID: "remote-qwen"}},
		FallbackToLocal: false,
	}

	caller := &fakeComputeTextCaller{
		handlers: map[string]func(int) (*proto.TextInferenceResponse, error){
			tc35: func(attempt int) (*proto.TextInferenceResponse, error) {
				if attempt == 1 {
					return &proto.TextInferenceResponse{Content: "第一版远程摘要", DurationMs: 200, ModelName: "qwen3.5:9b"}, nil
				}
				return &proto.TextInferenceResponse{Content: "第二版远程摘要", DurationMs: 210, ModelName: "qwen3.5:9b"}, nil
			},
		},
	}
	index := &tableSummaryIndex{}
	f.svc.tenantRepo = questionTenantRepository{TenantRepository: f.svc.tenantRepo}
	f.svc.config = questionTestConfig()
	f.svc.modelService = tableSummaryModelService{model: &stubLocalChat{name: "local-unused", reply: "unused"}}
	f.svc.retrieveEngine = tableSummaryRegistry{index: index}
	f.svc.documentReader = &fakeDocReaderWithText{caller: caller}

	payload, err := json.Marshal(types.SummaryGenerationPayload{
		TenantID: 7, KnowledgeID: "doc", KnowledgeBaseID: "kb", Attempt: 1,
	})
	require.NoError(t, err)

	require.NoError(t, f.svc.ProcessSummaryGeneration(types.WithTaskRetryMetadata(f.ctx, 0, 3), asynq.NewTask(types.TypeSummaryGeneration, payload)))
	// Simulate retry/re-execution when a summary chunk was already created earlier.
	require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id = ?", "doc").Update("summary_status", types.SummaryStatusPending).Error)
	require.NoError(t, f.svc.ProcessSummaryGeneration(types.WithTaskRetryMetadata(f.ctx, 1, 3), asynq.NewTask(types.TypeSummaryGeneration, payload)))

	var summaryChunks []types.Chunk
	require.NoError(t, f.db.Where("knowledge_id = ? AND chunk_type = ?", "doc", types.ChunkTypeSummary).Find(&summaryChunks).Error)
	require.Len(t, summaryChunks, 1, "retrying summary generation must update existing summary chunk in place")
	require.Contains(t, summaryChunks[0].Content, "第二版远程摘要")
}
