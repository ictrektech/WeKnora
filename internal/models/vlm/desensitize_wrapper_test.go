package vlm

import (
	"context"
	"encoding/base64"
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

type captureImageVLM struct {
	prompt string
	images [][]byte
}

func (v *captureImageVLM) GetModelName() string { return "test" }
func (v *captureImageVLM) GetModelID() string   { return "vlm-record-id" }
func (v *captureImageVLM) Predict(_ context.Context, images [][]byte, prompt string) (string, error) {
	v.images = images
	v.prompt = prompt
	return "ok", nil
}

func TestDesensitizeVLMSanitizesImages(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	imageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path == "/api/v1/desensitize/image" {
			imageCalls++
			if payload["mime_type"] != "image/png" {
				t.Fatalf("unexpected image mime type: %v", payload["mime_type"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"image_base64": base64.StdEncoding.EncodeToString([]byte("masked-image")),
				"mime_type":    "image/png",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": payload["messages"]})
	}))
	defer server.Close()

	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	inner := &captureImageVLM{}
	wrapper, err := wrapVLMDesensitize(inner, &Config{
		DesensitizeEnabled: true,
		DesensitizeImage:   true,
		DesensitizeBaseURL: server.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapper.Predict(context.Background(), [][]byte{pngBytes}, "识别"); err != nil {
		t.Fatal(err)
	}
	if imageCalls != 1 {
		t.Fatalf("expected 1 image desensitization call, got %d", imageCalls)
	}
	if string(inner.images[0]) != "masked-image" {
		t.Fatalf("expected sanitized image bytes, got %q", inner.images[0])
	}
	if inner.prompt != "识别" {
		t.Fatalf("unexpected prompt sent to VLM: %q", inner.prompt)
	}
}
