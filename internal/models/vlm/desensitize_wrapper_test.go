package vlm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

type capturePromptVLM struct {
	prompt string
}

func (v *capturePromptVLM) GetModelName() string { return "test" }
func (v *capturePromptVLM) GetModelID() string   { return "vlm-record-id" }
func (v *capturePromptVLM) Predict(_ context.Context, _ [][]byte, prompt string) (string, error) {
	v.prompt = prompt
	return "ok", nil
}

func TestDesensitizeVLMSanitizesTextPrompt(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		payload.Messages[0].Content = strings.ReplaceAll(
			payload.Messages[0].Content, "13812345678", "[PHONE_NUMBER]",
		)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"messages": payload.Messages})
	}))
	defer server.Close()

	inner := &capturePromptVLM{}
	wrapper, err := wrapVLMDesensitize(inner, &Config{
		DesensitizeEnabled: true,
		DesensitizeBaseURL: server.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapper.Predict(context.Background(), nil, "识别 13812345678"); err != nil {
		t.Fatal(err)
	}
	if inner.prompt != "识别 [PHONE_NUMBER]" {
		t.Fatalf("unexpected prompt sent to VLM: %q", inner.prompt)
	}
}
