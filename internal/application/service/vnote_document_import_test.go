package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type vnoteOCRTestModel struct {
	vlm.VLM
	pages  int
	failOn int
}

func (m *vnoteOCRTestModel) Predict(context.Context, [][]byte, string) (string, error) {
	m.pages++
	if m.pages == m.failOn {
		return "", errors.New("OCR rejected")
	}
	return fmt.Sprintf("Recognized page %02d.", m.pages), nil
}

type vnoteOCRTestModels struct {
	interfaces.ModelService
	model vlm.VLM
}

func (m vnoteOCRTestModels) GetVLMModel(context.Context, string) (vlm.VLM, error) {
	return m.model, nil
}

func TestVNoteDocumentOCRKeepsMixedPagesInOrder(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey,
		&types.Tenant{DefaultSettings: &types.DefaultSettings{VLMModelID: "vision"}})
	sourceTypes := make(map[string]string)
	raw := &types.ReadResult{Metadata: map[string]string{"scanned_page_count": "10"}}
	for i := 1; i <= 10; i++ {
		filename := fmt.Sprintf("scan_page_%d.jpg", i)
		ref := "images/" + filename
		sourceTypes[ref] = "scanned_pdf"
		raw.MarkdownContent += fmt.Sprintf("Text layer %02d.\n\n![%s](%s)\n\n", i, filename, ref)
		raw.ImageRefs = append(raw.ImageRefs, types.ImageRef{
			Filename: filename, OriginalRef: ref, ImageData: []byte{0xff, 0xd8, 0xff, 0xe0},
		})
	}
	encoded, _ := json.Marshal(sourceTypes)
	raw.Metadata["image_source_types"] = string(encoded)

	model := &vnoteOCRTestModel{}
	parser := &VNoteDocumentParser{models: vnoteOCRTestModels{model: model}}
	var done, total int
	result, err := parser.prepareResult(ctx, raw, func(current, count int) { done, total = current, count })
	if err != nil {
		t.Fatal(err)
	}
	if done != 10 || total != 10 || len(result.Images) != 10 {
		t.Fatalf("OCR progress %d/%d, images %d", done, total, len(result.Images))
	}
	for i := 1; i <= 10; i++ {
		pair := fmt.Sprintf("Text layer %02d.\n\nRecognized page %02d.", i, i)
		if !strings.Contains(result.Markdown, pair) {
			t.Fatalf("text and OCR out of order on page %d", i)
		}
	}

	parser.models = vnoteOCRTestModels{model: &vnoteOCRTestModel{failOn: 9}}
	result, err = parser.prepareResult(ctx, raw, func(int, int) {})
	if result != nil || !errors.Is(err, ErrVNoteDocumentOCR) {
		t.Fatalf("OCR failure on page 9 returned result=%v error=%v", result != nil, err)
	}
}
