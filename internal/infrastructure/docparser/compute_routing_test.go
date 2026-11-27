package docparser

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestComputeRoutingWeightedAndOrdered(t *testing.T) {
	r, err := NewComputeRouter(nil, filepath.Join(t.TempDir(), "routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := []computeCandidate{
		{node: ComputeNode{Origin: "a", Weight: 3}},
		{node: ComputeNode{Origin: "b", Weight: 1}},
	}
	cfg := ComputeRoutingConfig{Strategy: weightedComputeStrategy}
	counts := map[string]int{}
	for range 40 {
		counts[r.choose(cfg, nodes).node.Origin]++
	}
	if counts["a"] != 30 || counts["b"] != 10 {
		t.Fatalf("weight 3:1 yielded %v", counts)
	}
	if got := r.choose(cfg, nodes[1:]).node.Origin; got != "b" {
		t.Fatalf("unavailable first node: chose %s", got)
	}
	nodes[0].node.Weight = 1
	clear(r.scores)
	for _, expected := range []string{"a", "b", "a", "b"} {
		if got := r.choose(cfg, nodes).node.Origin; got != expected {
			t.Fatalf("weight 1:1: expected %s, got %s", expected, got)
		}
	}
	cfg.Strategy = localComputeStrategy
	if got := r.choose(cfg, nodes).node.Origin; got != "a" {
		t.Fatalf("ordered route chose %s", got)
	}
}

func TestComputeRoutingPersistsAndRedactsNodeCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	r, err := NewComputeRouter(nil, path)
	if err != nil {
		t.Fatal(err)
	}
	const token = "private-node-token"
	initial := ComputeRoutingConfig{Enabled: true, Strategy: localComputeStrategy, Nodes: []ComputeNode{
		{Origin: "https://192.168.1.35:1180/", Token: token, Weight: 1},
	}}
	if err := r.Save(initial); err != nil {
		t.Fatal(err)
	}
	masked := r.Config()
	if masked.Nodes[0].Token != "" || masked.Nodes[0].Origin != "https://192.168.1.35:1180" {
		t.Fatalf("unexpected public configuration")
	}
	masked.Nodes[0].Weight = 3
	if err := r.Save(masked); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(masked); !errors.Is(err, ErrComputeRoutingConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	got, err := NewComputeRouter(nil, path)
	if err != nil {
		t.Fatal(err)
	}
	loaded := got.snapshot().Nodes[0]
	if loaded.Weight != 3 || loaded.Token != token {
		t.Fatalf("saved node settings did not survive reload")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected permissions %v", info.Mode().Perm())
	}
}

type localComputeTestReader struct{ GRPCDocumentReader }

func (r *localComputeTestReader) Read(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
	return &types.ReadResult{MarkdownContent: "local test document"}, nil
}

func (r *localComputeTestReader) ListEngines(context.Context, map[string]string) ([]types.ParserEngineInfo, error) {
	return []types.ParserEngineInfo{{Name: "builtin", FileTypes: []string{"pdf"}, Available: true}}, nil
}

func TestComputeRoutingSkipsUnavailableNodeBeforeReading(t *testing.T) {
	r, err := NewComputeRouter(&localComputeTestReader{}, filepath.Join(t.TempDir(), "routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = r.Save(ComputeRoutingConfig{Enabled: true, Strategy: localComputeStrategy, Nodes: []ComputeNode{
		{Origin: "https://192.0.2.1:1", Token: "private-node-token", Weight: 1},
		{Origin: ComputeNodeLocal, Weight: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Read(t.Context(), &types.ReadRequest{FileType: "pdf", ParserEngine: "builtin"})
	if err != nil || result.Metadata["docreader_node"] != ComputeNodeLocal {
		t.Fatalf("expected local fallback, got %v, %v", result, err)
	}
}

type computeTestServer struct {
	proto.UnimplementedDocReaderServer
}

func (s computeTestServer) ListEngines(ctx context.Context, _ *proto.ListEnginesRequest) (*proto.ListEnginesResponse, error) {
	if got := metadata.ValueFromIncomingContext(ctx, "authorization"); len(got) != 1 || got[0] != "Bearer compute-test-secret" {
		return nil, status.Error(codes.Unauthenticated, "invalid credential")
	}
	return &proto.ListEnginesResponse{Engines: []*proto.ParserEngineInfo{
		{Name: "builtin", FileTypes: []string{"pdf"}, Available: true},
	}}, nil
}

func (s computeTestServer) ReadStream(req *proto.ReadRequest, stream proto.DocReader_ReadStreamServer) error {
	if string(req.GetFileContent()) == "fail-after-send" {
		return status.Error(codes.Unavailable, "remote outcome unknown")
	}
	if err := stream.Send(&proto.ReadStreamResponse{Payload: &proto.ReadStreamResponse_Meta{Meta: &proto.ReadStreamMeta{
		MarkdownContent: "remote document", ImageCount: 1,
	}}}); err != nil {
		return err
	}
	return stream.Send(&proto.ReadStreamResponse{Payload: &proto.ReadStreamResponse_Image{Image: &proto.ImageRef{
		Filename: "figure.png", ImageData: []byte{1, 2, 3},
	}}})
}

func TestComputeRoutingPinnedTLSStreamsImagesAndDoesNotReplay(t *testing.T) {
	fixture := httptest.NewTLSServer(nil)
	cert := fixture.TLS.Certificates[0]
	fixture.Close()
	fingerprint := sha256.Sum256(cert.Certificate[0])
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}})))
	proto.RegisterDocReaderServer(server, computeTestServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	r, err := NewComputeRouter(&localComputeTestReader{}, filepath.Join(t.TempDir(), "routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The integration fixture uses a loopback listener, so it fills the
	// in-memory routing configuration directly.
	remote := ComputeNode{Origin: "https://" + listener.Addr().String(), Weight: 1,
		Token: "compute-test-secret", Fingerprint: hex.EncodeToString(fingerprint[:])}
	r.config = ComputeRoutingConfig{Enabled: true, Strategy: localComputeStrategy, Nodes: []ComputeNode{
		remote, {Origin: ComputeNodeLocal, Weight: 1},
	}}
	req := &types.ReadRequest{FileType: "pdf", ParserEngine: "builtin", FileContent: []byte("fixture")}
	result, err := r.Read(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.MarkdownContent != "remote document" || result.Metadata["docreader_node"] != remote.Origin ||
		len(result.ImageRefs) != 1 || string(result.ImageRefs[0].ImageData) != "\x01\x02\x03" {
		t.Fatal("expected remote text, node metadata and streamed image")
	}
	req.FileContent = []byte("fail-after-send")
	if result, err = r.Read(t.Context(), req); result != nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("read must fail without invoking the local backup, got result=%v error=%v", result, err)
	}
}
