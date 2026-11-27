package docparser

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sharingTestReader struct{ computeTestServer }

func (sharingTestReader) GetInfo(context.Context, *proto.GetInfoRequest) (*proto.GetInfoResponse, error) {
	return &proto.GetInfoResponse{Version: "fixture"}, nil
}

func (sharingTestReader) ListEngines(context.Context, *proto.ListEnginesRequest) (*proto.ListEnginesResponse, error) {
	return &proto.ListEnginesResponse{Engines: []*proto.ParserEngineInfo{
		{Name: "builtin", FileTypes: []string{"pdf"}, Available: true},
	}}, nil
}

func (s sharingTestReader) ReadStream(req *proto.ReadRequest, stream proto.DocReader_ReadStreamServer) error {
	if string(req.GetFileContent()) != "scanned-fixture" {
		return s.computeTestServer.ReadStream(req, stream)
	}
	if err := stream.Send(&proto.ReadStreamResponse{Payload: &proto.ReadStreamResponse_Meta{Meta: &proto.ReadStreamMeta{
		ImageCount: 1, Metadata: map[string]string{"image_source_types": `{"images/page.jpg":"scanned_pdf"}`},
	}}}); err != nil {
		return err
	}
	return stream.Send(&proto.ReadStreamResponse{Payload: &proto.ReadStreamResponse_Image{Image: &proto.ImageRef{
		Filename: "page.jpg", ImageData: []byte{1, 2, 3},
	}}})
}

func TestComputeSharingPairsOnceAndServesLocalReader(t *testing.T) {
	stub, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	localServer := grpc.NewServer()
	proto.RegisterDocReaderServer(localServer, sharingTestReader{})
	go func() { _ = localServer.Serve(stub) }()
	t.Cleanup(localServer.Stop)
	local, err := NewGRPCDocumentReader(stub.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Close() })

	addresses, _ := net.InterfaceAddrs()
	var listener net.Listener
	for _, address := range addresses {
		ip, _, _ := net.ParseCIDR(address.String())
		if ip != nil && ip.To4() != nil && !ip.IsLoopback() && ip.IsGlobalUnicast() {
			listener, err = net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
			if err == nil {
				break
			}
		}
	}
	if listener == nil {
		t.Skip("No private-network listener for remote pairing fixture")
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := "https://" + listener.Addr().String()
	providerFile := filepath.Join(t.TempDir(), "provider.json")
	share, err := NewComputeSharing(providerFile, listener.Addr().String(), local)
	if err != nil {
		t.Fatal(err)
	}
	appListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appListener.Close() })
	selected := ComputeDocumentModel{TenantID: 42, ModelID: "fixture-vision"}
	app := share.NewDocumentGateway(func(_ context.Context, binding ComputeDocumentModel) (DocumentImageAnalyzer, error) {
		if binding != selected {
			return nil, errors.New("unexpected provider model binding")
		}
		return func(_ context.Context, image []byte, scanned bool, language string) (DocumentImageAnalysisResult, error) {
			if len(image) != 3 || !scanned || language != "中文" {
				return DocumentImageAnalysisResult{}, errors.New("unexpected image analysis input")
			}
			return DocumentImageAnalysisResult{
				OCRText:               "识别出的正文",
				Description:           "图片描述",
				OCRDurationMs:         1250,
				DescriptionDurationMs: 840,
			}, nil
		}, nil
	})
	go func() { _ = app.Serve(appListener) }()
	t.Cleanup(app.Stop)
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: appListener.Addr().String()})
	proxy.Transport = &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	edge := httptest.NewUnstartedServer(proxy)
	_ = edge.Listener.Close()
	edge.Listener = listener
	edge.EnableHTTP2 = true
	edge.StartTLS()
	t.Cleanup(edge.Close)

	consumer, err := NewComputeRouter(&localComputeTestReader{}, filepath.Join(t.TempDir(), "consumer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := share.IssuePairingCode(t.Context(), origin); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected sharing disabled, got %v", err)
	}
	enabled := true
	if err := share.Update(&enabled, false); err != nil {
		t.Fatal(err)
	}
	code, err := share.IssuePairingCode(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	node, err := consumer.PairNode(t.Context(), code.Code, 0, true)
	if err != nil || node.Origin != origin || node.Version != "fixture" {
		t.Fatalf("pairing failed: %v %v", node, err)
	}
	if _, err := consumer.PairNode(t.Context(), code.Code, 1, true); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected one-time code to be spent, got %v", err)
	}
	expired, err := share.IssuePairingCode(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	share.mu.Lock()
	share.pendingUntil = time.Now().Add(-time.Second)
	share.mu.Unlock()
	if _, err := consumer.PairNode(t.Context(), expired.Code, 1, true); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected expired code to fail, got %v", err)
	}
	cfg := consumer.Config()
	if !cfg.Enabled || cfg.Strategy != localComputeStrategy || cfg.Nodes[0].Token != "" {
		t.Fatal("adding the first node must enable ordered routing when requested and mask credentials")
	}
	if len(node.Capabilities) != 0 {
		t.Fatalf("an existing DocReader pairing granted a new capability: %v", node.Capabilities)
	}
	existingCredential := consumer.snapshot().Nodes[0].Token
	auth := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+existingCredential))
	if err := share.authorizeCapability(auth, false, true); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected document processing to require separate provider opt-in, got %v", err)
	}
	remote, err := newComputeReader(consumer.snapshot().Nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	request := &proto.DocumentComputeRequest{
		Document:        &proto.ReadRequest{FileType: "pdf", FileContent: []byte("scanned-fixture")},
		RecognizeImages: true, DescriptionLanguage: "中文",
	}
	compute := proto.NewDocumentComputeClient(remote.conn)
	frames, recvErr := compute.Process(t.Context(), request)
	if recvErr == nil {
		_, recvErr = frames.Recv()
	}
	if status.Code(recvErr) != codes.PermissionDenied {
		t.Fatalf("expected gated processing, got %v", recvErr)
	}
	documentProcessing := true
	if err := share.UpdateDocumentProcessing(nil, &documentProcessing, nil, false); !errors.Is(err, ErrComputeDocumentModelRequired) {
		t.Fatalf("expected an explicit provider model at enablement, got %v", err)
	}
	if err := share.UpdateDocumentProcessing(nil, &documentProcessing, &selected, false); err != nil {
		t.Fatal(err)
	}
	probed, err := consumer.Probe(t.Context(), cfg.Nodes[0])
	if err != nil || len(probed.Capabilities) != 1 || probed.Capabilities[0] != computeCapabilityDocumentProcessing {
		t.Fatalf("existing pairing could not discover the new capability: %v %v", probed.Capabilities, err)
	}
	frames, err = compute.Process(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := frames.Recv()
	if err != nil || frame.GetMeta().GetImageCount() != 1 {
		t.Fatalf("expected parse metadata, got %v %v", frame, err)
	}
	frame, err = frames.Recv()
	image := frame.GetImage()
	if err != nil || !image.GetOcrPerformed() || !image.GetDescriptionPerformed() ||
		image.GetOcrText() != "识别出的正文" || image.GetDescription() != "图片描述" ||
		image.GetOcrDurationMs() != 1250 || image.GetDescriptionDurationMs() != 840 {
		t.Fatalf("expected scanned page and its recognition results, got %v %v", image, err)
	}
	if _, err := frames.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected successful stream completion, got %v", err)
	}
	failingRequest := &proto.DocumentComputeRequest{
		Document: request.GetDocument(), RecognizeImages: true, DescriptionLanguage: "unexpected",
	}
	frames, err = compute.Process(t.Context(), failingRequest)
	if err != nil {
		t.Fatal(err)
	}
	frame, err = frames.Recv()
	if err != nil || frame.GetMeta() == nil {
		t.Fatalf("expected metadata before the recognition failure, got %v %v", frame, err)
	}
	if _, err := frames.Recv(); status.Code(err) != codes.Unavailable {
		t.Fatalf("expected a failed stream instead of an image-only success, got %v", err)
	}
	cfg.Nodes[0].DocumentProcessing = true
	if err := consumer.Save(cfg); err != nil {
		t.Fatal(err)
	}
	documentRequest := &types.ReadRequest{
		FileType: "pdf", FileContent: []byte("scanned-fixture"),
		DocumentCompute: &types.DocumentComputeOptions{RecognizeImages: true, DescriptionLanguage: "中文"},
	}
	computed, err := consumer.Read(t.Context(), documentRequest)
	if err != nil || !computed.DocumentComputed || computed.Metadata["docreader_node"] != origin ||
		len(computed.ImageRefs) != 1 || computed.ImageRefs[0].Analysis == nil ||
		computed.ImageRefs[0].Analysis.OCRText != "识别出的正文" {
		t.Fatalf("the upload reader did not use the enabled document processing node: %v %v", computed, err)
	}
	documentRequest.DocumentCompute.DescriptionLanguage = "unexpected"
	if result, err := consumer.Read(t.Context(), documentRequest); result != nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("the upload reader accepted a partial document compute stream: %v %v", result, err)
	}
	result, err := consumer.Read(t.Context(), &types.ReadRequest{
		FileType: "pdf", ParserEngine: "builtin", FileContent: []byte("fixture"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.MarkdownContent != "remote document" || len(result.ImageRefs) != 1 {
		t.Fatal("expected the provider's local reader text and streamed image")
	}
	reloaded, err := NewComputeSharing(providerFile, "", local)
	if err != nil {
		t.Fatal(err)
	}
	saved := reloaded.Status()
	if !saved.Enabled || !saved.DocumentProcessing || saved.DocumentModelID != selected.ModelID || saved.PairedNodes != 1 {
		t.Fatalf("expected provider authorization to survive restart, got %v", err)
	}
	if err := reloaded.authorizeCapability(auth, false, true); err != nil {
		t.Fatal(err)
	}
	documentProcessing = false
	if err := reloaded.UpdateDocumentProcessing(nil, &documentProcessing, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.authorizeCapability(auth, false, true); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected document processing to stop without revoking the pairing, got %v", err)
	}
	if err := reloaded.authorize(auth, false); err != nil {
		t.Fatal(err)
	}
	enabled = false
	if err := reloaded.Update(&enabled, false); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.authorize(auth, false); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected sharing disabled, got %v", err)
	}
	enabled = true
	if err := reloaded.Update(&enabled, true); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.authorize(auth, false); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected credential revoked, got %v", err)
	}
}
