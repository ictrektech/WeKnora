package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

var (
	// ErrVNoteDocumentParse indicates that the document could not be parsed.
	ErrVNoteDocumentParse = errors.New("document_parse_failed")
	// ErrVNoteDocumentOCRConfig indicates that no vision model is available for OCR.
	ErrVNoteDocumentOCRConfig = errors.New("document_ocr_unavailable")
	// ErrVNoteDocumentOCR indicates that scanned-page recognition failed.
	ErrVNoteDocumentOCR = errors.New("document_ocr_failed")
	// ErrVNoteDocumentEmpty indicates that conversion produced no editable text.
	ErrVNoteDocumentEmpty = errors.New("document_empty")
)

// VNoteDocumentImage describes an image extracted during conversion.
type VNoteDocumentImage struct {
	Ref      string `json:"ref"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Data     []byte `json:"-"`
}

// VNoteDocumentResult contains the editable Markdown and its images.
type VNoteDocumentResult struct {
	Markdown string               `json:"markdown"`
	Images   []VNoteDocumentImage `json:"images"`
}

// VNoteDocumentParser converts V-Note imports using the tenant's parser and OCR settings.
type VNoteDocumentParser struct {
	reader  interfaces.DocumentReader
	tenants interfaces.TenantService
	models  interfaces.ModelService
}

// NewVNoteDocumentParser creates a parser for V-Note imports.
func NewVNoteDocumentParser(
	reader interfaces.DocumentReader, tenants interfaces.TenantService, models interfaces.ModelService,
) *VNoteDocumentParser {
	return &VNoteDocumentParser{reader: reader, tenants: tenants, models: models}
}

// Parse converts an uploaded document and reports scanned-page progress.
func (p *VNoteDocumentParser) Parse(
	ctx context.Context, tenantID uint64, userID, name string, data []byte, progress func(done, total int),
) (*VNoteDocumentResult, error) {
	ctx = types.WithCaller(ctx, types.Caller{UserID: userID, TenantID: tenantID, Role: types.TenantRoleOwner})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: userID})
	ctx = types.WithExecutionTenant(ctx, tenantID)
	tenant, err := p.tenants.GetTenantByID(ctx, tenantID)
	if err != nil || tenant == nil {
		return nil, ErrVNoteDocumentParse
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	config := tenant.ParserEngineConfig
	engine := config.ResolveChatParserEngine(ext)
	overrides := config.ToOverridesMap()
	reader, err := docparser.NewReader(ctx, engine, ext, false, docparser.ReaderDeps{
		Remote: p.reader, Overrides: overrides, WeKnoraCloudCredentials: p.tenants.GetWeKnoraCloudCredentials,
	})
	if err != nil {
		return nil, ErrVNoteDocumentParse
	}
	raw, err := reader.Read(ctx, &types.ReadRequest{
		FileContent: data, FileName: name, FileType: ext,
		ParserEngine: engine, ParserEngineOverrides: overrides,
	})
	if err != nil || raw == nil || raw.Error != "" {
		return nil, ErrVNoteDocumentParse
	}
	return p.prepareResult(ctx, raw, progress)
}

func (p *VNoteDocumentParser) prepareResult(
	ctx context.Context, raw *types.ReadResult, progress func(done, total int),
) (*VNoteDocumentResult, error) {
	markdown := docparser.NormalizeHTMLTables(raw.MarkdownContent)
	result := &VNoteDocumentResult{Images: make([]VNoteDocumentImage, 0, len(raw.ImageRefs))}
	var sourceTypes map[string]string
	_ = json.Unmarshal([]byte(raw.Metadata["image_source_types"]), &sourceTypes)
	var model vlm.VLM
	totalPages, _ := strconv.Atoi(raw.Metadata["scanned_page_count"])
	scannedPages := 0
	if totalPages > 0 {
		progress(0, totalPages)
	}
	for _, image := range raw.ImageRefs {
		ref := image.OriginalRef
		if ref == "" {
			ref = "images/" + image.Filename
		}
		// Only use inline image bytes supplied by the parser. Paths and URLs
		// in a document never authorize reading other files or network targets.
		if len(image.ImageData) == 0 {
			if sourceTypes[ref] == "scanned_pdf" || strings.Contains(markdown, "]("+ref+")") {
				return nil, ErrVNoteDocumentParse
			}
			continue
		}
		mime := http.DetectContentType(image.ImageData)
		switch mime {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			if strings.Contains(markdown, "]("+ref+")") {
				return nil, ErrVNoteDocumentParse
			}
			continue
		}
		if sourceTypes[ref] == "scanned_pdf" {
			if ctx.Err() != nil {
				return nil, ErrVNoteDocumentOCR
			}
			if model == nil {
				model = p.defaultOCRModel(ctx)
			}
			if model == nil {
				return nil, ErrVNoteDocumentOCRConfig
			}
			text, err := model.Predict(ctx, [][]byte{image.ImageData}, vlmOCRScannedPDFPrompt)
			if err == nil {
				text, err = validateOCRText(text)
			}
			if err != nil {
				return nil, ErrVNoteDocumentOCR
			}
			placeholder := fmt.Sprintf("![%s](%s)", image.Filename, ref)
			if !strings.Contains(markdown, placeholder) {
				return nil, ErrVNoteDocumentOCR
			}
			if text != "" {
				markdown = strings.Replace(markdown, placeholder, text+"\n\n"+placeholder, 1)
			}
			scannedPages++
			progress(scannedPages, totalPages)
		}
		result.Images = append(result.Images, VNoteDocumentImage{
			Ref: ref, Filename: image.Filename, MimeType: mime, Data: image.ImageData,
		})
	}
	// Some OCR engines return finished Markdown without scanned page image
	// placeholders. The built-in PDF parser reports its scanned-page count.
	if expected := raw.Metadata["scanned_page_count"]; expected != "" && expected != fmt.Sprint(scannedPages) {
		return nil, ErrVNoteDocumentOCR
	}
	if approxTextContentRunes(markdown) == 0 {
		return nil, ErrVNoteDocumentEmpty
	}
	result.Markdown = markdown
	return result, nil
}

func (p *VNoteDocumentParser) defaultOCRModel(ctx context.Context) vlm.VLM {
	tenant, ok := types.TenantInfoFromContext(ctx)
	if ok && tenant.DefaultSettings != nil && tenant.DefaultSettings.VLMModelID != "" {
		model, _ := p.models.GetVLMModel(ctx, tenant.DefaultSettings.VLMModelID)
		return model
	}
	models, err := p.models.ListModels(ctx)
	if err != nil {
		return nil
	}
	for _, candidate := range models {
		if candidate.Type == types.ModelTypeVLLM && candidate.IsDefault {
			model, _ := p.models.GetVLMModel(ctx, candidate.ID)
			return model
		}
	}
	return nil
}
