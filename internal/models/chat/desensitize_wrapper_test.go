package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

type captureDesensitizeChat struct {
	messages []Message
	calls    int
}

func (c *captureDesensitizeChat) GetModelName() string { return "test" }
func (c *captureDesensitizeChat) GetModelID() string   { return "test-id" }
func (c *captureDesensitizeChat) Chat(_ context.Context, messages []Message, _ *ChatOptions) (*types.ChatResponse, error) {
	c.calls++
	c.messages = messages
	return &types.ChatResponse{}, nil
}
func (c *captureDesensitizeChat) ChatStream(_ context.Context, messages []Message, _ *ChatOptions) (<-chan types.StreamResponse, error) {
	c.calls++
	c.messages = messages
	ch := make(chan types.StreamResponse)
	close(ch)
	return ch, nil
}

func withDesensitizeTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	return httptest.NewServer(handler)
}

func TestDesensitizeChatSanitizesRegularAndMultimodalText(t *testing.T) {
	var gotNER bool
	server := withDesensitizeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request desensitizeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		gotNER = request.Options.NER
		for i := range request.Messages {
			request.Messages[i].Content = strings.ReplaceAll(request.Messages[i].Content, "13812345678", "[PHONE]")
		}
		_ = json.NewEncoder(w).Encode(desensitizeResponse{Messages: request.Messages})
	})
	defer server.Close()

	inner := &captureDesensitizeChat{}
	wrapper, err := wrapChatDesensitize(inner, &ChatConfig{
		DesensitizeEnabled: true,
		DesensitizeNER:     true,
		DesensitizeBaseURL: server.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	original := []Message{{
		Role:    "user",
		Content: "电话 13812345678",
		MultiContent: []MessageContentPart{{
			Type: "text", Text: "再次输入 13812345678",
		}},
	}}
	if _, err := wrapper.Chat(context.Background(), original, nil); err != nil {
		t.Fatal(err)
	}
	if !gotNER {
		t.Fatal("expected NER option")
	}
	if inner.messages[0].Content != "电话 [PHONE]" || inner.messages[0].MultiContent[0].Text != "再次输入 [PHONE]" {
		t.Fatalf("unexpected sanitized messages: %#v", inner.messages)
	}
	if original[0].Content != "电话 13812345678" {
		t.Fatal("wrapper mutated original messages")
	}
}

func TestDesensitizeChatBlocksProviderCallWhenServiceFails(t *testing.T) {
	server := withDesensitizeTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	defer server.Close()

	inner := &captureDesensitizeChat{}
	wrapper, err := wrapChatDesensitize(inner, &ChatConfig{
		DesensitizeEnabled: true,
		DesensitizeBaseURL: server.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapper.ChatStream(context.Background(), []Message{{Role: "user", Content: "secret"}}, nil); err == nil {
		t.Fatal("expected service error")
	}
	if inner.calls != 0 {
		t.Fatalf("provider called %d times after desensitization failure", inner.calls)
	}
}
