package docparser

import (
	"context"
	"errors"
	"io"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

type computeGateway struct {
	proto.UnimplementedDocReaderServer
	share              *ComputeSharing
	documentProcessing bool
	textInference      bool
}

const (
	computeCapabilityHeader             = "hybrag-compute-capability"
	computeCapabilityDocumentProcessing = "document_processing"
	computeCapabilityTextInference      = "text_inference"
)

func (s *ComputeSharing) NewGateway() *grpc.Server {
	return s.NewComputeGateway(nil, nil)
}

func (s *ComputeSharing) NewDocumentGateway(analyzer DocumentImageAnalyzerFactory) *grpc.Server {
	return s.NewComputeGateway(analyzer, nil)
}

func (s *ComputeSharing) NewComputeGateway(analyzer DocumentImageAnalyzerFactory, textExecutor ComputeTextExecutor) *grpc.Server {
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(getMaxMessageSize()),
		grpc.MaxSendMsgSize(getMaxMessageSize()),
		grpc.InTapHandle(func(ctx context.Context, info *tap.Info) (context.Context, error) {
			incoming := metadata.NewIncomingContext(ctx, info.Header)
			return ctx, s.authorizeMethod(incoming, info.FullMethodName)
		}),
	)
	gateway := &computeGateway{
		share:              s,
		documentProcessing: analyzer != nil,
		textInference:      textExecutor != nil,
	}
	proto.RegisterDocReaderServer(server, gateway)
	if analyzer != nil || textExecutor != nil {
		proto.RegisterDocumentComputeServer(server, &documentComputeGateway{
			reader:       gateway,
			analyzer:     analyzer,
			textExecutor: textExecutor,
		})
	}
	return server
}

func (g *computeGateway) client() (proto.DocReaderClient, error) {
	local := g.share.local
	local.mu.RLock()
	defer local.mu.RUnlock()
	if local.client == nil {
		return nil, errNotConnected
	}
	return local.client, nil
}

func (g *computeGateway) GetInfo(ctx context.Context, req *proto.GetInfoRequest) (*proto.GetInfoResponse, error) {
	client, err := g.client()
	if err != nil {
		return nil, err
	}
	info, err := client.GetInfo(ctx, req)
	if err != nil {
		return info, err
	}
	headers := metadata.MD{}
	if isComputePairing(ctx) {
		credential, err := g.share.redeem(ctx)
		if err != nil {
			return nil, err
		}
		headers.Set(computeCredentialHeader, credential)
	}
	sharing := g.share.Status()
	if g.documentProcessing && sharing.Enabled && sharing.DocumentProcessing {
		headers.Append(computeCapabilityHeader, computeCapabilityDocumentProcessing)
	}
	if g.textInference && sharing.Enabled && sharing.TextInference {
		headers.Append(computeCapabilityHeader, computeCapabilityTextInference)
	}
	if len(headers) != 0 {
		if err := grpc.SetHeader(ctx, headers); err != nil {
			return nil, err
		}
	}
	return info, nil
}

func (g *computeGateway) ListEngines(ctx context.Context, req *proto.ListEnginesRequest) (*proto.ListEnginesResponse, error) {
	if err := checkSharedParserOverrides(req.GetConfigOverrides()); err != nil {
		return nil, err
	}
	client, err := g.client()
	if err != nil {
		return nil, err
	}
	return client.ListEngines(ctx, req)
}

func (g *computeGateway) Read(ctx context.Context, req *proto.ReadRequest) (*proto.ReadResponse, error) {
	if err := checkSharedDocument(req); err != nil {
		return nil, err
	}
	client, err := g.client()
	if err != nil {
		return nil, err
	}
	return client.Read(ctx, req)
}

func (g *computeGateway) ReadStream(req *proto.ReadRequest, stream proto.DocReader_ReadStreamServer) error {
	if err := checkSharedDocument(req); err != nil {
		return err
	}
	client, err := g.client()
	if err != nil {
		return err
	}
	frames, err := client.ReadStream(stream.Context(), req)
	if err != nil {
		return err
	}
	for {
		frame, err := frames.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
}

func (g *computeGateway) PreprocessImage(ctx context.Context, req *proto.PreprocessImageRequest) (*proto.PreprocessImageResponse, error) {
	client, err := g.client()
	if err != nil {
		return nil, err
	}
	return client.PreprocessImage(ctx, req)
}

func checkSharedDocument(req *proto.ReadRequest) error {
	if req.GetUrl() != "" || len(req.GetFileContent()) == 0 {
		return status.Error(codes.InvalidArgument, "shared DocReader accepts uploaded files")
	}
	if len(req.GetFileContent()) > getMaxFileSize() {
		return status.Error(codes.ResourceExhausted, "file exceeds the shared DocReader limit")
	}
	return checkSharedParserOverrides(req.GetConfig().GetParserEngineOverrides())
}

func checkSharedParserOverrides(overrides map[string]string) error {
	// OpenDataLoader is the DocReader-side engine with a caller-provided
	// outbound HTTP endpoint. Apply the provider's own network policy.
	if target := overrides["odl_hybrid_url"]; target != "" {
		if err := utils.ValidateURLForSSRF(target); err != nil {
			return status.Error(codes.InvalidArgument, "parser endpoint is disallowed by the provider")
		}
	}
	return nil
}
