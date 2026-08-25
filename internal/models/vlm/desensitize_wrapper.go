package vlm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

type desensitizeVLM struct {
	inner   VLM
	baseURL string
	ner     bool
	image   bool
}

func (w *desensitizeVLM) GetModelName() string { return w.inner.GetModelName() }
func (w *desensitizeVLM) GetModelID() string   { return w.inner.GetModelID() }

func (w *desensitizeVLM) Predict(ctx context.Context, images [][]byte, prompt string) (string, error) {
	sanitized, err := chat.DesensitizeText(ctx, w.baseURL, w.ner, prompt)
	if err != nil {
		return "", err
	}
	if w.image {
		sanitizedImages := make([][]byte, len(images))
		for i, img := range images {
			mimeType := http.DetectContentType(img)
			sanitizedImg, _, imgErr := chat.DesensitizeImage(ctx, w.baseURL, w.ner, img, mimeType)
			if imgErr != nil {
				return "", imgErr
			}
			sanitizedImages[i] = sanitizedImg
		}
		images = sanitizedImages
	}
	return w.inner.Predict(ctx, images, sanitized)
}

func wrapVLMDesensitize(v VLM, config *Config, err error) (VLM, error) {
	if err != nil || v == nil || config == nil || !config.DesensitizeEnabled {
		return v, err
	}
	if strings.TrimSpace(config.DesensitizeBaseURL) == "" {
		return nil, fmt.Errorf("desensitization is enabled but service URL is empty")
	}
	return &desensitizeVLM{
		inner:   v,
		baseURL: config.DesensitizeBaseURL,
		ner:     config.DesensitizeNER,
		image:   config.DesensitizeImage,
	}, nil
}
