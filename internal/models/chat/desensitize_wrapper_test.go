package chat

import (
	"context"
	"encoding/base64"
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
	var gotRoles []string
	server := withDesensitizeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var request desensitizeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		gotNER = request.Options.NER
		for i := range request.Messages {
			gotRoles = append(gotRoles, request.Messages[i].Role)
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
		Role:    "assistant",
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
	for _, role := range gotRoles {
		if role != "user" {
			t.Fatalf("expected all text to be submitted for sanitization as user content, got roles %v", gotRoles)
		}
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

func TestDesensitizeChatSanitizesAttachedImages(t *testing.T) {
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	imageRequests := 0
	server := withDesensitizeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case desensitizePath:
			var request desensitizeRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(desensitizeResponse{Messages: request.Messages})
		case desensitizeImagePath:
			var request imageDesensitizeRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			imageRequests++
			if request.MimeType == "" {
				t.Fatal("image request missing mime type")
			}
			response := request
			response.ImageBase64 = base64.StdEncoding.EncodeToString([]byte("masked-image"))
			response.MimeType = "image/png"
			_ = json.NewEncoder(w).Encode(response)
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	})
	defer server.Close()

	inner := &captureDesensitizeChat{}
	wrapper, err := wrapChatDesensitize(inner, &ChatConfig{
		DesensitizeEnabled: true,
		DesensitizeImage:   true,
		DesensitizeBaseURL: server.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	remoteURL := "https://example.com/photo.jpg"
	original := []Message{{
		Role:    "user",
		Content: "看看这些图",
		Images:  []string{"data:image/png;base64," + pngBase64, remoteURL},
		MultiContent: []MessageContentPart{{
			Type:     "image_url",
			ImageURL: &ImageURL{URL: "data:image/png;base64," + pngBase64},
		}},
	}}
	if _, err := wrapper.Chat(context.Background(), original, nil); err != nil {
		t.Fatal(err)
	}
	wantDataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("masked-image"))
	if inner.messages[0].Images[0] != wantDataURI {
		t.Fatalf("expected stored image to be sanitized, got %q", inner.messages[0].Images[0])
	}
	if inner.messages[0].Images[1] != remoteURL {
		t.Fatalf("remote URL should pass through unchanged, got %q", inner.messages[0].Images[1])
	}
	if inner.messages[0].MultiContent[0].ImageURL.URL != wantDataURI {
		t.Fatalf("expected multimodal image part to be sanitized, got %q",
			inner.messages[0].MultiContent[0].ImageURL.URL)
	}
	if imageRequests != 2 {
		t.Fatalf("expected 2 image desensitization calls, got %d", imageRequests)
	}
	if original[0].Images[0] != "data:image/png;base64,"+pngBase64 {
		t.Fatal("wrapper mutated original image reference")
	}
	if original[0].MultiContent[0].ImageURL.URL != "data:image/png;base64,"+pngBase64 {
		t.Fatal("wrapper mutated original multimodal image reference")
	}
}

func TestDesensitizeChatBlocksProviderCallWhenImageServiceFails(t *testing.T) {
	server := withDesensitizeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == desensitizeImagePath {
			http.Error(w, "ocr unavailable", http.StatusServiceUnavailable)
			return
		}
		var request desensitizeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(desensitizeResponse{Messages: request.Messages})
	})
	defer server.Close()

	inner := &captureDesensitizeChat{}
	wrapper, err := wrapChatDesensitize(inner, &ChatConfig{
		DesensitizeEnabled: true,
		DesensitizeImage:   true,
		DesensitizeBaseURL: server.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapper.ChatStream(context.Background(), []Message{{
		Role:    "user",
		Content: "看图",
		Images:  []string{"data:image/png;base64,aGVsbG8="},
	}}, nil); err == nil {
		t.Fatal("expected image desensitization error")
	}
	if inner.calls != 0 {
		t.Fatalf("provider called %d times after image desensitization failure", inner.calls)
	}
}
