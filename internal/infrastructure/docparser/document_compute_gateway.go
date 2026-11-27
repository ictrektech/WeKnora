package docparser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/docreader/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	maxTextInferenceMessages     = 64
	maxTextInferenceContentBytes = 2 << 20
	maxTextInferenceTokens       = 32768
)

// DocumentImageAnalysisResult carries the recognized text and executor-measured
// durations for one PDF image.
type DocumentImageAnalysisResult struct {
	OCRText               string
	Description           string
	OCRDurationMs         int64
	DescriptionDurationMs int64
}

// DocumentImageAnalyzer calls one provider-selected vision model.
type DocumentImageAnalyzer func(ctx context.Context, image []byte, scanned bool, language string) (DocumentImageAnalysisResult, error)

type DocumentImageAnalyzerFactory func(context.Context, ComputeDocumentModel) (DocumentImageAnalyzer, error)

// ComputeTextExecutor serves shared text-inference model discovery and generation
// using the provider's configured workspace models (for example via local V-Scheduler).
type ComputeTextExecutor interface {
	ListModels(ctx context.Context, bindings []ComputeTextModel) ([]ComputeTextModelInfo, error)
	Generate(ctx context.Context, binding ComputeTextModel, req *proto.TextInferenceRequest) (*proto.TextInferenceResponse, error)
}

type documentComputeGateway struct {
	proto.UnimplementedDocumentComputeServer
	reader       *computeGateway
	analyzer     DocumentImageAnalyzerFactory
	textExecutor ComputeTextExecutor
}

func (g *documentComputeGateway) Process(req *proto.DocumentComputeRequest, stream proto.DocumentCompute_ProcessServer) error {
	if g.analyzer == nil {
		return status.Error(codes.Unimplemented, "document processing is unavailable on this node")
	}
	doc := req.GetDocument()
	if err := checkSharedDocument(doc); err != nil {
		return err
	}
	fileType := strings.TrimPrefix(strings.ToLower(doc.GetFileType()), ".")
	engine := doc.GetConfig().GetParserEngine()
	if fileType != "pdf" || (engine != "" && engine != "builtin") {
		return status.Error(codes.InvalidArgument, "document processing currently supports PDF with the built-in parser")
	}
	if len(req.GetDescriptionLanguage()) > 64 {
		return status.Error(codes.InvalidArgument, "description language is too long")
	}

	ctx := stream.Context()
	binding, enabled := g.reader.share.documentModel()
	if !enabled {
		return status.Error(codes.PermissionDenied, "document processing sharing is disabled")
	}
	var analyze DocumentImageAnalyzer
	client, err := g.reader.client()
	if err != nil {
		return err
	}
	parsed, err := client.ReadStream(ctx, doc)
	if err != nil {
		return err
	}
	first, err := parsed.Recv()
	if errors.Is(err, io.EOF) {
		return status.Error(codes.DataLoss, "document parser returned no metadata")
	}
	if err != nil {
		return err
	}
	meta := first.GetMeta()
	if meta == nil {
		return status.Error(codes.DataLoss, "document parser returned an invalid stream")
	}
	meta.ImageDirPath = ""
	if err := stream.Send(&proto.DocumentComputeFrame{Content: &proto.DocumentComputeFrame_Meta{Meta: meta}}); err != nil || meta.GetError() != "" {
		return err
	}

	sources, err := ReadPDFImageSources(meta.GetMetadata())
	if err != nil {
		return status.Error(codes.DataLoss, "document parser returned invalid image source types")
	}
	for {
		frame, err := parsed.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		ref := frame.GetImage()
		if ref == nil || len(ref.GetImageData()) == 0 {
			return status.Error(codes.DataLoss, "document parser returned an image without data")
		}
		ref.StorageKey = ""
		image := &proto.DocumentComputeImage{Image: ref}
		if req.GetRecognizeImages() {
			if analyze == nil {
				analyze, err = g.analyzer(ctx, binding)
				if err != nil || analyze == nil {
					return status.Error(codes.Unavailable, "the provider's vision model is unavailable")
				}
			}
			scanned := sources.Scanned(ref.GetOriginalRef(), ref.GetFilename())
			analysis, err := analyze(ctx, ref.GetImageData(), scanned, req.GetDescriptionLanguage())
			if err != nil {
				if ctx.Err() != nil {
					return status.FromContextError(ctx.Err()).Err()
				}
				return status.Error(codes.Unavailable, "the provider could not recognize a PDF image")
			}
			image.OcrText, image.Description = analysis.OCRText, analysis.Description
			image.OcrPerformed, image.DescriptionPerformed = scanned, true
			image.OcrDurationMs, image.DescriptionDurationMs = analysis.OCRDurationMs, analysis.DescriptionDurationMs
		}
		if err := stream.Send(&proto.DocumentComputeFrame{Content: &proto.DocumentComputeFrame_Image{Image: image}}); err != nil {
			return err
		}
	}
}

func (g *documentComputeGateway) ListTextModels(ctx context.Context, _ *proto.ListTextModelsRequest) (*proto.ListTextModelsResponse, error) {
	if g.textExecutor == nil {
		return nil, status.Error(codes.Unimplemented, "text inference is unavailable on this node")
	}
	bindings, enabled := g.reader.share.textModels()
	if !enabled {
		return nil, status.Error(codes.PermissionDenied, "text inference sharing is disabled")
	}
	models, err := g.textExecutor.ListModels(ctx, bindings)
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Error(codes.Unavailable, "failed to list shared text inference models")
	}
	resp := &proto.ListTextModelsResponse{
		Models: make([]*proto.TextModelInfo, 0, len(models)),
	}
	for _, m := range models {
		resp.Models = append(resp.Models, &proto.TextModelInfo{
			Id:            m.ID,
			Name:          m.Name,
			DisplayName:   m.DisplayName,
			ContextWindow: int32(m.ContextWindow),
		})
	}
	return resp, nil
}

func (g *documentComputeGateway) GenerateText(ctx context.Context, req *proto.TextInferenceRequest) (*proto.TextInferenceResponse, error) {
	if g.textExecutor == nil {
		return nil, status.Error(codes.Unimplemented, "text inference is unavailable on this node")
	}
	if err := validateTextInferenceRequest(req); err != nil {
		return nil, err
	}
	binding, authorized := g.reader.share.textModel(req.GetModelId())
	if !authorized {
		return nil, status.Error(codes.PermissionDenied, "the requested text model is not authorized on this node")
	}
	resp, err := g.textExecutor.Generate(ctx, binding, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		if busy, ok := ClassifyComputeBusyError(err); ok {
			trailers := metadata.Pairs(computeErrorCodeHeader, ComputeErrorCodeBusy)
			if busy.RetryAfter > 0 {
				secs := int(math.Ceil(busy.RetryAfter.Seconds()))
				if secs < 1 {
					secs = 1
				}
				trailers.Append(computeRetryAfterHeader, strconv.Itoa(secs))
			}
			_ = grpc.SetTrailer(ctx, trailers)
			return nil, status.Errorf(codes.ResourceExhausted, "%s: provider compute resource is busy", ComputeErrorCodeBusy)
		}
		return nil, status.Errorf(codes.Unavailable, "provider text inference failed: %v", err)
	}
	return resp, nil
}

func validateTextInferenceRequest(req *proto.TextInferenceRequest) error {
	if req == nil || strings.TrimSpace(req.GetModelId()) == "" {
		return status.Error(codes.InvalidArgument, "model_id is required")
	}
	messages := req.GetMessages()
	if len(messages) == 0 || len(messages) > maxTextInferenceMessages {
		return status.Error(codes.InvalidArgument, "messages must contain 1 to 64 entries")
	}
	totalBytes := 0
	for _, msg := range messages {
		if msg == nil {
			return status.Error(codes.InvalidArgument, "message cannot be empty")
		}
		switch strings.TrimSpace(msg.GetRole()) {
		case "system", "user", "assistant":
		default:
			return status.Error(codes.InvalidArgument, "unsupported message role")
		}
		totalBytes += len(msg.GetContent())
		if totalBytes > maxTextInferenceContentBytes {
			return status.Error(codes.InvalidArgument, "text inference prompt exceeds size limit")
		}
	}
	if req.GetMaxTokens() < 0 || req.GetMaxTokens() > maxTextInferenceTokens {
		return status.Error(codes.InvalidArgument, "max_tokens is out of range")
	}
	return nil
}

type PDFImageSources struct {
	types       map[string]string
	defaultType string
}

func ReadPDFImageSources(metadata map[string]string) (PDFImageSources, error) {
	sources := PDFImageSources{defaultType: metadata["image_source_type"]}
	if raw := metadata["image_source_types"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &sources.types); err != nil {
			return PDFImageSources{}, err
		}
	}
	return sources, nil
}

func (s PDFImageSources) Scanned(originalRef, filename string) bool {
	if len(s.types) == 0 {
		return s.defaultType == "scanned_pdf"
	}
	if kind := s.types[originalRef]; kind != "" {
		return kind == "scanned_pdf"
	}
	return s.types["images/"+filename] == "scanned_pdf"
}
