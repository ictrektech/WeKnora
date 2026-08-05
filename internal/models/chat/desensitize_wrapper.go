package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

const (
	desensitizePath        = "/api/v1/desensitize"
	maxDesensitizeResponse = 4 << 20
)

type desensitizeChat struct {
	inner   Chat
	baseURL string
	ner     bool
	client  *http.Client
}

type desensitizeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type desensitizeRequest struct {
	Messages []desensitizeMessage `json:"messages"`
	Options  struct {
		NER bool `json:"ner"`
	} `json:"options"`
}

type desensitizeResponse struct {
	Messages []desensitizeMessage `json:"messages"`
}

type textTarget struct {
	messageIndex int
	partIndex    int
	isPart       bool
}

func (w *desensitizeChat) GetModelName() string  { return w.inner.GetModelName() }
func (w *desensitizeChat) GetModelID() string    { return w.inner.GetModelID() }
func (w *desensitizeChat) GetLimiterKey() string { return modelLimiterKey(w.inner) }

func (w *desensitizeChat) Chat(
	ctx context.Context,
	messages []Message,
	opts *ChatOptions,
) (*types.ChatResponse, error) {
	sanitized, err := w.sanitize(ctx, messages)
	if err != nil {
		return nil, err
	}
	return w.inner.Chat(ctx, sanitized, opts)
}

func (w *desensitizeChat) ChatStream(
	ctx context.Context,
	messages []Message,
	opts *ChatOptions,
) (<-chan types.StreamResponse, error) {
	sanitized, err := w.sanitize(ctx, messages)
	if err != nil {
		return nil, err
	}
	return w.inner.ChatStream(ctx, sanitized, opts)
}

func (w *desensitizeChat) sanitize(ctx context.Context, messages []Message) ([]Message, error) {
	requestMessages, targets := flattenText(messages)
	if len(requestMessages) == 0 {
		return messages, nil
	}

	payload := desensitizeRequest{Messages: requestMessages}
	payload.Options.NER = w.ner
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode desensitization request: %w", err)
	}

	endpoint := strings.TrimRight(w.baseURL, "/") + desensitizePath
	if err := secutils.ValidateURLForSSRF(endpoint); err != nil {
		return nil, fmt.Errorf("desensitization service URL blocked by SSRF policy: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create desensitization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("desensitization service request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxDesensitizeResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read desensitization response: %w", err)
	}
	if len(responseBody) > maxDesensitizeResponse {
		return nil, fmt.Errorf("desensitization response exceeds %d bytes", maxDesensitizeResponse)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("desensitization service returned HTTP %d", resp.StatusCode)
	}

	var decoded desensitizeResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode desensitization response: %w", err)
	}
	if len(decoded.Messages) != len(targets) {
		return nil, fmt.Errorf(
			"desensitization service returned %d messages, expected %d",
			len(decoded.Messages), len(targets),
		)
	}

	sanitized := cloneMessages(messages)
	for i, target := range targets {
		if target.isPart {
			sanitized[target.messageIndex].MultiContent[target.partIndex].Text = decoded.Messages[i].Content
		} else {
			sanitized[target.messageIndex].Content = decoded.Messages[i].Content
		}
	}
	return sanitized, nil
}

func flattenText(messages []Message) ([]desensitizeMessage, []textTarget) {
	requestMessages := make([]desensitizeMessage, 0, len(messages))
	targets := make([]textTarget, 0, len(messages))
	for messageIndex, message := range messages {
		if message.Content != "" {
			requestMessages = append(requestMessages, desensitizeMessage{Role: message.Role, Content: message.Content})
			targets = append(targets, textTarget{messageIndex: messageIndex})
		}
		for partIndex, part := range message.MultiContent {
			if part.Type != "text" || part.Text == "" {
				continue
			}
			requestMessages = append(requestMessages, desensitizeMessage{Role: message.Role, Content: part.Text})
			targets = append(targets, textTarget{messageIndex: messageIndex, partIndex: partIndex, isPart: true})
		}
	}
	return requestMessages, targets
}

func cloneMessages(messages []Message) []Message {
	cloned := append([]Message(nil), messages...)
	for i := range cloned {
		cloned[i].MultiContent = append([]MessageContentPart(nil), messages[i].MultiContent...)
	}
	return cloned
}

func wrapChatDesensitize(c Chat, config *ChatConfig, err error) (Chat, error) {
	if err != nil || c == nil || config == nil || !config.DesensitizeEnabled {
		return c, err
	}
	baseURL := strings.TrimSpace(config.DesensitizeBaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("desensitization is enabled but service URL is empty")
	}
	clientConfig := secutils.DefaultSSRFSafeHTTPClientConfig()
	clientConfig.Timeout = 15 * time.Second
	return &desensitizeChat{
		inner:   c,
		baseURL: baseURL,
		ner:     config.DesensitizeNER,
		client:  secutils.NewSSRFSafeHTTPClient(clientConfig),
	}, nil
}
