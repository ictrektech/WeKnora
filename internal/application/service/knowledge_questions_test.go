package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type questionChatAPI = chat.Chat

type questionResponseChat struct {
	questionChatAPI
	responses []*types.ChatResponse
	options   *chat.ChatOptions
}

func (m *questionResponseChat) Chat(_ context.Context, _ []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	m.options = opts
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

type questionTenantRepository struct {
	interfaces.TenantRepository
}

func (questionTenantRepository) GetTenantByID(ctx context.Context, id uint64) (*types.Tenant, error) {
	return (tableSummaryTenantService{}).GetTenantByID(ctx, id)
}

func questionTestConfig() *config.Config {
	return &config.Config{Conversation: &config.ConversationConfig{
		GenerateQuestionsPrompt: "Generate {{question_count}} questions about {{content}}.",
	}}
}

func TestGenerateQuestionsRejectsEmptyOutput(t *testing.T) {
	for _, response := range []*types.ChatResponse{
		{Content: "", FinishReason: "length"},
		{Content: " \n\t", FinishReason: "stop"},
		{Content: "1.\n2.", FinishReason: "stop"},
	} {
		model := &questionResponseChat{responses: []*types.ChatResponse{response}}
		svc := &knowledgeService{config: questionTestConfig()}
		questions, err := svc.generateQuestionsWithContext(context.Background(), model, "document body", "", "", "doc", 3, "")
		require.ErrorContains(t, err, "no usable questions")
		require.ErrorContains(t, err, "finish_reason="+response.FinishReason)
		require.Empty(t, questions)
		require.NotNil(t, model.options.Thinking)
		require.False(t, *model.options.Thinking)
	}
}

func TestQuestionTasksRetryOnlyWhenAllGenerationFails(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "batch"}[batched], func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				responses []*types.ChatResponse
				wantError bool
				wantCount int
				retry     int
			}{
				{"empty", []*types.ChatResponse{{FinishReason: "length"}, {FinishReason: "length"}}, true, 0, 0},
				{"empty_final", []*types.ChatResponse{{FinishReason: "length"}, {FinishReason: "length"}}, true, 0, 3},
				{"partial", []*types.ChatResponse{{Content: "劳动者享有哪些权利？"}, {FinishReason: "length"}}, false, 1, 0},
				{"success", []*types.ChatResponse{{Content: "劳动者享有哪些权利？"}, {Content: "用人单位承担哪些义务？"}}, false, 2, 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f := newDocumentWriteFixture(t)
					require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id = ?", "doc").Updates(map[string]any{
						"parse_status": types.ParseStatusFinalizing, "pending_subtasks_count": 1,
					}).Error)
					require.NoError(t, f.db.Create(&types.Chunk{
						ID: "second", TenantID: 7, KnowledgeID: "doc", KnowledgeBaseID: "kb",
						ChunkType: types.ChunkTypeText, Content: "second document body",
					}).Error)
					model := &questionResponseChat{responses: tc.responses}
					index := &tableSummaryIndex{}
					f.svc.tenantRepo = questionTenantRepository{TenantRepository: f.svc.tenantRepo}
					f.svc.config = questionTestConfig()
					f.svc.modelService = tableSummaryModelService{model: model}
					f.svc.retrieveEngine = tableSummaryRegistry{index: index}
					taskPayload := types.QuestionGenerationPayload{
						TenantID: 7, KnowledgeID: "doc", KnowledgeBaseID: "kb", QuestionCount: 3,
					}
					if batched {
						taskPayload.ChunkIDs = []string{"chunk", "second"}
					}
					payload, err := json.Marshal(taskPayload)
					require.NoError(t, err)
					ctx := types.WithTaskRetryMetadata(f.ctx, tc.retry, 3)
					err = f.svc.ProcessQuestionGeneration(ctx, asynq.NewTask(types.TypeQuestionGeneration, payload))
					if tc.wantError {
						require.ErrorContains(t, err, "no usable questions")
					} else {
						require.NoError(t, err)
					}
					require.Len(t, index.indexed, tc.wantCount)
					knowledge, err := f.repo.GetKnowledgeByID(ctx, 7, "doc")
					require.NoError(t, err)
					if tc.wantError && tc.retry < 3 {
						require.Equal(t, 1, knowledge.PendingSubtasksCount, "retry must keep the subtask slot")
					} else {
						require.Zero(t, knowledge.PendingSubtasksCount)
					}
				})
			}
		})
	}
}
