package chat

import (
	"bytes"
	"context"
	"encoding/base64"
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
	desensitizePath             = "/api/v1/desensitize"
	desensitizeImagePath        = "/api/v1/desensitize/image"
	maxDesensitizeResponse      = 4 << 20
	maxDesensitizeImageResponse = 32 << 20
)

type desensitizeChat struct {
	inner       Chat
	baseURL     string
	ner         bool
	image       bool
	client      *http.Client
	imageClient *http.Client
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

type imageDesensitizeRequest struct {
	ImageBase64 string `json:"image_base64"`
	MimeType    string `json:"mime_type"`
	NER         bool   `json:"ner"`
}

type imageDesensitizeResponse struct {
	ImageBase64 string `json:"image_base64"`
	MimeType    string `json:"mime_type"`
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
	var sanitized []Message
	if len(requestMessages) == 0 {
		if !w.image {
			return messages, nil
		}
		sanitized = cloneMessages(messages)
	} else {
		processed, err := w.sanitizeText(ctx, messages, requestMessages, targets)
		if err != nil {
			return nil, err
		}
		sanitized = processed
	}
	if w.image {
		if err := w.sanitizeImages(ctx, sanitized); err != nil {
			return nil, err
		}
	}
	return sanitized, nil
}

func (w *desensitizeChat) sanitizeText(
	ctx context.Context, messages []Message, requestMessages []desensitizeMessage, targets []textTarget,
) ([]Message, error) {
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

// sanitizeImages routes every image attached to the messages through the
// image desensitization endpoint. It runs after the text pass on the cloned
// message slice, so originals stay untouched.
func (w *desensitizeChat) sanitizeImages(ctx context.Context, messages []Message) error {
	for messageIndex := range messages {
		msg := &messages[messageIndex]
		for i, ref := range msg.Images {
			sanitized, err := w.sanitizeImageRef(ctx, ref)
			if err != nil {
				return err
			}
			msg.Images[i] = sanitized
		}
		for partIndex := range msg.MultiContent {
			part := &msg.MultiContent[partIndex]
			if part.Type != "image_url" || part.ImageURL == nil {
				continue
			}
			sanitized, err := w.sanitizeImageRef(ctx, part.ImageURL.URL)
			if err != nil {
				return err
			}
			part.ImageURL.URL = sanitized
		}
	}
	return nil
}

// sanitizeImageRef desensitizes one chat image reference. Remote http(s) URLs
// are returned unchanged: downloading them here would widen the SSRF surface,
// and the provider receives the same URL either way. Application-stored images
// (local://, resource://, storage://) are resolved to bytes exactly like the
// provider would, desensitized, and swapped for a self-contained data URI that
// both the OpenAI-compatible and Ollama paths accept.
func (w *desensitizeChat) sanitizeImageRef(ctx context.Context, ref string) (string, error) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, nil
	}
	mimeType, decoded, ok := splitDataURI(resolveImageURLForLLM(ref))
	if !ok {
		return ref, nil
	}
	sanitized, sanitizedMime, err := desensitizeImageBytes(
		ctx, w.imageClient, w.baseURL, w.ner, decoded, mimeType,
	)
	if err != nil {
		return "", err
	}
	return "data:" + sanitizedMime + ";base64," + base64.StdEncoding.EncodeToString(sanitized), nil
}

// splitDataURI returns the mime type and decoded payload of a base64 data URI.
func splitDataURI(uri string) (string, []byte, bool) {
	const prefix = "data:"
	if !strings.HasPrefix(uri, prefix) {
		return "", nil, false
	}
	sep := strings.Index(uri, ";base64,")
	if sep < 0 {
		return "", nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(uri[sep+len(";base64,"):])
	if err != nil {
		return "", nil, false
	}
	return uri[len(prefix):sep], decoded, true
}

func desensitizeImageBytes(
	ctx context.Context, client *http.Client, baseURL string, ner bool, data []byte, mimeType string,
) ([]byte, string, error) {
	payload := imageDesensitizeRequest{
		ImageBase64: base64.StdEncoding.EncodeToString(data),
		MimeType:    mimeType,
		NER:         ner,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("encode image desensitization request: %w", err)
	}

	endpoint := strings.TrimRight(baseURL, "/") + desensitizeImagePath
	if err := secutils.ValidateURLForSSRF(endpoint); err != nil {
		return nil, "", fmt.Errorf("desensitization service URL blocked by SSRF policy: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("create image desensitization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("image desensitization service request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxDesensitizeImageResponse+1))
	if err != nil {
		return nil, "", fmt.Errorf("read image desensitization response: %w", err)
	}
	if len(responseBody) > maxDesensitizeImageResponse {
		return nil, "", fmt.Errorf("image desensitization response exceeds %d bytes", maxDesensitizeImageResponse)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("image desensitization service returned HTTP %d", resp.StatusCode)
	}

	var decodedResp imageDesensitizeResponse
	if err := json.Unmarshal(responseBody, &decodedResp); err != nil {
		return nil, "", fmt.Errorf("decode image desensitization response: %w", err)
	}
	sanitized, err := base64.StdEncoding.DecodeString(decodedResp.ImageBase64)
	if err != nil {
		return nil, "", fmt.Errorf("decode image desensitization payload: %w", err)
	}
	if decodedResp.MimeType == "" {
		decodedResp.MimeType = mimeType
	}
	return sanitized, decodedResp.MimeType, nil
}

func flattenText(messages []Message) ([]desensitizeMessage, []textTarget) {
	requestMessages := make([]desensitizeMessage, 0, len(messages))
	targets := make([]textTarget, 0, len(messages))
	for messageIndex, message := range messages {
		if message.Content != "" {
			// The service intentionally skips assistant content. For outbound
			// model protection every textual prompt segment is sensitive input,
			// including system and assistant history, so submit each segment as
			// user text and restore it to its original message slot below.
			requestMessages = append(requestMessages, desensitizeMessage{Role: "user", Content: message.Content})
			targets = append(targets, textTarget{messageIndex: messageIndex})
		}
		for partIndex, part := range message.MultiContent {
			if part.Type != "text" || part.Text == "" {
				continue
			}
			requestMessages = append(requestMessages, desensitizeMessage{Role: "user", Content: part.Text})
			targets = append(targets, textTarget{messageIndex: messageIndex, partIndex: partIndex, isPart: true})
		}
	}
	return requestMessages, targets
}

func cloneMessages(messages []Message) []Message {
	cloned := append([]Message(nil), messages...)
	for i := range cloned {
		cloned[i].MultiContent = append([]MessageContentPart(nil), messages[i].MultiContent...)
		for j := range cloned[i].MultiContent {
			if original := messages[i].MultiContent[j].ImageURL; original != nil {
				copied := *original
				cloned[i].MultiContent[j].ImageURL = &copied
			}
		}
		cloned[i].Images = append([]string(nil), messages[i].Images...)
	}
	return cloned
}

// DesensitizeText applies the same fail-closed policy to a standalone prompt,
// such as the textual instruction sent with an image to a VLM.
func DesensitizeText(ctx context.Context, baseURL string, ner bool, text string) (string, error) {
	clientConfig := secutils.DefaultSSRFSafeHTTPClientConfig()
	clientConfig.Timeout = 15 * time.Second
	w := &desensitizeChat{
		baseURL: strings.TrimSpace(baseURL),
		ner:     ner,
		client:  secutils.NewSSRFSafeHTTPClient(clientConfig),
	}
	messages, err := w.sanitize(ctx, []Message{{Role: "user", Content: text}})
	if err != nil {
		return "", err
	}
	return messages[0].Content, nil
}

// DesensitizeImage applies the same fail-closed policy to raw image bytes,
// such as the images handed to a VLM for document parsing. The returned mime
// type may differ from the input because the service re-encodes the masked
// image.
func DesensitizeImage(
	ctx context.Context, baseURL string, ner bool, data []byte, mimeType string,
) ([]byte, string, error) {
	clientConfig := secutils.DefaultSSRFSafeHTTPClientConfig()
	// Image redaction runs OCR before masking, which is far slower than the
	// text rules engine; give it a wider budget than the text endpoint.
	clientConfig.Timeout = 60 * time.Second
	return desensitizeImageBytes(
		ctx, secutils.NewSSRFSafeHTTPClient(clientConfig), strings.TrimSpace(baseURL), ner, data, mimeType,
	)
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
	imageClientConfig := secutils.DefaultSSRFSafeHTTPClientConfig()
	imageClientConfig.Timeout = 60 * time.Second
	return &desensitizeChat{
		inner:       c,
		baseURL:     baseURL,
		ner:         config.DesensitizeNER,
		image:       config.DesensitizeImage,
		client:      secutils.NewSSRFSafeHTTPClient(clientConfig),
		imageClient: secutils.NewSSRFSafeHTTPClient(imageClientConfig),
	}, nil
}
