package docparser

import (
	"context"
	"errors"
	"io"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (p *GRPCDocumentReader) computeDocument(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	if len(req.FileContent) > getMaxFileSize() {
		return nil, status.Error(codes.ResourceExhausted, "file exceeds the document processing limit")
	}
	options := req.DocumentCompute
	doc := documentReadProto(req)
	doc.Config.ParserEngine = BuiltinEngineName
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := proto.NewDocumentComputeClient(p.conn).Process(ctx, &proto.DocumentComputeRequest{
		Document: doc, RecognizeImages: options.RecognizeImages, DescriptionLanguage: options.DescriptionLanguage,
	})
	if err != nil {
		return nil, err
	}
	first, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return nil, status.Error(codes.DataLoss, "document processing returned no metadata")
	}
	if err != nil {
		return nil, err
	}
	meta := first.GetMeta()
	if meta == nil {
		return nil, status.Error(codes.DataLoss, "document processing returned an invalid stream")
	}
	result := &types.ReadResult{
		MarkdownContent: meta.GetMarkdownContent(), Metadata: meta.GetMetadata(),
		SourceBlocks: sourceBlocksFromProto(meta.GetSourceBlocks()), Error: meta.GetError(),
		DocumentComputed: true,
	}
	seen := make(map[string]bool)
	sources, err := ReadPDFImageSources(meta.GetMetadata())
	if err != nil {
		return nil, status.Error(codes.DataLoss, "document processing returned invalid image source types")
	}
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if result.Error == "" && meta.GetImageCount() > uint32(len(result.ImageRefs)) {
				return nil, status.Error(codes.DataLoss, "document processing returned fewer images than declared")
			}
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		image := frame.GetImage()
		ref := image.GetImage()
		key := ref.GetOriginalRef()
		if key == "" {
			key = "images/" + ref.GetFilename()
		}
		if ref == nil || seen[key] || len(ref.GetImageData()) == 0 {
			return nil, status.Error(codes.DataLoss, "document processing returned an invalid image")
		}
		seen[key] = true
		var analysis *types.DocumentImageAnalysis
		if options.RecognizeImages {
			if !image.GetDescriptionPerformed() || image.GetDescription() == "" ||
				(sources.Scanned(ref.GetOriginalRef(), ref.GetFilename()) && !image.GetOcrPerformed()) {
				return nil, status.Error(codes.DataLoss, "document processing returned incomplete image recognition")
			}
			analysis = &types.DocumentImageAnalysis{
				OCRText:               image.GetOcrText(),
				Description:           image.GetDescription(),
				OCRPerformed:          image.GetOcrPerformed(),
				OCRDurationMs:         image.GetOcrDurationMs(),
				DescriptionDurationMs: image.GetDescriptionDurationMs(),
			}
		}
		result.ImageRefs = append(result.ImageRefs, types.ImageRef{
			Filename: ref.GetFilename(), OriginalRef: key, MimeType: ref.GetMimeType(),
			ImageData: ref.GetImageData(), Analysis: analysis,
		})
	}
}
