package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gopkg.in/yaml.v3"
)

type summaryContentCaptureChat struct {
	messages []chat.Message
}

func (m *summaryContentCaptureChat) Chat(
	_ context.Context,
	messages []chat.Message,
	_ *chat.ChatOptions,
) (*types.ChatResponse, error) {
	m.messages = append([]chat.Message(nil), messages...)
	return &types.ChatResponse{Content: "summary"}, nil
}

func (m *summaryContentCaptureChat) ChatStream(
	context.Context,
	[]chat.Message,
	*chat.ChatOptions,
) (<-chan types.StreamResponse, error) {
	return nil, nil
}

func (m *summaryContentCaptureChat) GetModelName() string { return "summary-capture" }
func (m *summaryContentCaptureChat) GetModelID() string   { return "summary-capture" }

type summaryImageInfoChunkRepo struct {
	interfaces.ChunkRepository
}

func (summaryImageInfoChunkRepo) ListChunksByParentIDs(
	context.Context, uint64, []string,
) ([]*types.Chunk, error) {
	return nil, nil
}

func TestGetSummaryReconstructsTableChunksWithSyntheticHeaders(t *testing.T) {
	header := "| Country | Capital |\n| --- | --- |\n"
	rowOne := "| Alpha Republic | North City |\n"
	rowTwo := "| Beta Federation | East Harbor |\n"
	rowThree := "| Gamma State | South Port |\n"
	want := header + rowOne + rowTwo + rowThree

	firstContent := header + rowOne + rowTwo
	service := &knowledgeService{
		config: &config.Config{Conversation: &config.ConversationConfig{
			GenerateSummaryPrompt: "Summarize the document.",
		}},
		chunkRepo: summaryImageInfoChunkRepo{},
	}
	model := &summaryContentCaptureChat{}

	_, err := service.getSummary(context.Background(), model, &types.Knowledge{ID: "knowledge-1"}, []*types.Chunk{
		{
			ID: "first", Content: firstContent, ChunkIndex: 0,
			StartAt: 0, EndAt: len([]rune(firstContent)),
		},
		{
			// The repeated header is synthetic: StartAt points at row two in the source.
			ID: "second", Content: header + rowTwo + rowThree, ChunkIndex: 1,
			StartAt: len([]rune(header + rowOne)), EndAt: len([]rune(want)),
		},
	})
	if err != nil {
		t.Fatalf("getSummary() error = %v", err)
	}
	if len(model.messages) != 2 {
		t.Fatalf("summary model received %d messages, want 2", len(model.messages))
	}
	if got := model.messages[1].Content; got != want {
		t.Fatalf("summary content mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestGetSummaryPinsOutputLanguageAcrossSourceLanguages(t *testing.T) {
	t.Setenv("WEKNORA_LANGUAGE", "zh-CN")
	data, err := os.ReadFile("../../../config/prompt_templates/generate_summary.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var templates struct {
		Templates []config.PromptTemplate `yaml:"templates"`
	}
	if err := yaml.Unmarshal(data, &templates); err != nil {
		t.Fatal(err)
	}
	template := config.DefaultTemplate(templates.Templates)
	if template == nil || template.ID != "default_summary" {
		t.Fatal("default_summary template not found")
	}
	for _, tt := range []struct {
		locale, language, source string
	}{
		{"en-US", "English", "中华人民共和国劳动法规定劳动者的权益和用人单位的义务。"},
		{"zh-CN", "Chinese (Simplified)", "The Labor Law protects workers and regulates employment contracts."},
		{"zh-TW", "Chinese (Traditional)", "中华人民共和国劳动法规定劳动者的权益和用人单位的义务。"},
		{"", "Chinese (Simplified)", "The Labor Law protects workers and regulates employment contracts."},
	} {
		t.Run(tt.locale, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.LanguageContextKey, tt.locale)
			service := &knowledgeService{
				config: &config.Config{Conversation: &config.ConversationConfig{
					GenerateSummaryPrompt: template.Content,
				}},
				chunkRepo: summaryImageInfoChunkRepo{},
			}
			model := &summaryContentCaptureChat{}
			_, err := service.getSummary(ctx, model, &types.Knowledge{ID: "knowledge-language"}, []*types.Chunk{
				{ID: "text", Content: tt.source, ChunkType: types.ChunkTypeText},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(model.messages) != 2 {
				t.Fatalf("got %d messages, want 2", len(model.messages))
			}
			wantPrompt := strings.ReplaceAll(template.Content, "{{language}}", tt.language)
			if got := model.messages[0]; got.Role != "system" || got.Content != wantPrompt {
				t.Fatalf("system message did not render the template without additions: %#v", got)
			}
			if !strings.Contains(wantPrompt, "Write every field in "+tt.language+".") ||
				!strings.Contains(wantPrompt, "The target language "+tt.language+" takes precedence over the source document language.") {
				t.Fatalf("template did not explicitly require the target language: %q", wantPrompt)
			}
			if got := model.messages[1]; got.Role != "user" || got.Content != tt.source {
				t.Fatalf("source document was changed: %#v", got)
			}
		})
	}
}
