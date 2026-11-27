package docparser

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/models/api"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubTextExecutor struct {
	models  map[string]ComputeTextModelInfo
	busyCnt atomic.Int32
	calls   atomic.Int32
}

func (s *stubTextExecutor) ListModels(_ context.Context, bindings []ComputeTextModel) ([]ComputeTextModelInfo, error) {
	out := make([]ComputeTextModelInfo, 0, len(bindings))
	for _, b := range bindings {
		if info, ok := s.models[b.ModelID]; ok {
			out = append(out, info)
		}
	}
	return out, nil
}

func (s *stubTextExecutor) Generate(_ context.Context, binding ComputeTextModel, req *proto.TextInferenceRequest) (*proto.TextInferenceResponse, error) {
	s.calls.Add(1)
	if s.busyCnt.Load() > 0 {
		s.busyCnt.Add(-1)
		return nil, &api.HTTPError{
			StatusCode: http.StatusTooManyRequests,
			Body:       `{"code":"WAIT_COMPUTE_RESOURCE","message":"gpu busy"}`,
			Header:     http.Header{"Retry-After": []string{"2"}},
		}
	}
	return &proto.TextInferenceResponse{
		Content:      "remote reply for " + binding.ModelID + ":" + req.GetPurpose(),
		FinishReason: "stop",
		DurationMs:   345,
		ModelName:    s.models[binding.ModelID].Name,
	}, nil
}

func TestComputeSharingTextInferenceDiscoveryAndBusyPropagation(t *testing.T) {
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
	executor := &stubTextExecutor{
		models: map[string]ComputeTextModelInfo{
			"chat-a": {ID: "chat-a", Name: "qwen3.5:9b", DisplayName: "Qwen 3.5 9B", ContextWindow: 32768},
			"chat-b": {ID: "chat-b", Name: "qwen3.5:2b", DisplayName: "Qwen 3.5 2B", ContextWindow: 16384},
		},
	}
	appListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appListener.Close() })
	app := share.NewComputeGateway(nil, executor)
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

	consumerFile := filepath.Join(t.TempDir(), "consumer.json")
	consumer, err := NewComputeRouter(&localComputeTestReader{}, consumerFile)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Existing pairing without text inference enabled keeps original authorization only.
	enabled := true
	if err := share.Update(&enabled, false); err != nil {
		t.Fatal(err)
	}
	code, err := share.IssuePairingCode(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	paired, err := consumer.PairNode(t.Context(), code.Code, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(paired.Capabilities, computeCapabilityTextInference) || len(paired.TextModels) != 0 {
		t.Fatalf("existing pairing should not grant text_inference before provider opt-in: %+v", paired)
	}
	if _, err := consumer.GenerateText(t.Context(), origin, &proto.TextInferenceRequest{
		ModelId:  "chat-a",
		Messages: []*proto.TextChatMessage{{Role: "user", Content: "hi"}},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied before text inference enabled, got %v", err)
	}

	// 2. Enabling text inference requires at least one model.
	textOn := true
	if err := share.UpdateSharing(nil, nil, nil, &textOn, nil, false); !errors.Is(err, ErrComputeTextModelRequired) {
		t.Fatalf("expected ErrComputeTextModelRequired, got %v", err)
	}

	// 3. Provider authorizes chat-a; existing consumer Probes and discovers chat-a without re-pairing.
	if err := share.UpdateSharing(nil, nil, nil, &textOn, []ComputeTextModel{{TenantID: 7, ModelID: "chat-a"}}, false); err != nil {
		t.Fatal(err)
	}
	probed, err := consumer.Probe(t.Context(), consumer.Config().Nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(probed.Capabilities, computeCapabilityTextInference) {
		t.Fatalf("expected text_inference capability, got %v", probed.Capabilities)
	}
	if len(probed.TextModels) != 1 || probed.TextModels[0].ID != "chat-a" || probed.TextModels[0].Name != "qwen3.5:9b" {
		t.Fatalf("unexpected discovered models: %+v", probed.TextModels)
	}
	// Verify discovered catalog persisted and survived reload without bumping revision.
	reloadedConsumer, err := NewComputeRouter(&localComputeTestReader{}, consumerFile)
	if err != nil {
		t.Fatal(err)
	}
	if info, ok := reloadedConsumer.LookupTextModel(origin, "chat-a"); !ok || info.DisplayName != "Qwen 3.5 9B" {
		t.Fatalf("expected persisted discovered text model, got %+v %v", info, ok)
	}

	// 4. Unauthorized model chat-b is denied even though text inference is enabled.
	if _, err := consumer.GenerateText(t.Context(), origin, &proto.TextInferenceRequest{
		ModelId:  "chat-b",
		Messages: []*proto.TextChatMessage{{Role: "user", Content: "hi"}},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for unshared model chat-b, got %v", err)
	}

	// 5. Authorized model succeeds and reports executor-measured duration.
	resp, err := consumer.GenerateText(t.Context(), origin, &proto.TextInferenceRequest{
		ModelId:  "chat-a",
		Messages: []*proto.TextChatMessage{{Role: "user", Content: "summarize"}},
		Purpose:  "document_summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetContent() != "remote reply for chat-a:document_summary" || resp.GetDurationMs() != 345 {
		t.Fatalf("unexpected remote text inference response: %+v", resp)
	}

	// 6. V-Scheduler 429 WAIT_COMPUTE_RESOURCE propagates as ComputeBusyError with RetryAfter.
	executor.busyCnt.Store(1)
	_, err = consumer.GenerateText(t.Context(), origin, &proto.TextInferenceRequest{
		ModelId:  "chat-a",
		Messages: []*proto.TextChatMessage{{Role: "user", Content: "questions"}},
		Purpose:  "question_generation",
	})
	busy, isBusy := ClassifyComputeBusyError(err)
	if !isBusy || busy.Code != ComputeErrorCodeBusy || busy.RetryAfter != 2*time.Second {
		t.Fatalf("expected ComputeBusyError with 2s retry-after, got busy=%v err=%v", busy, err)
	}

	// 7. Stopping text inference revokes text_inference while keeping DocReader pairing alive.
	textOff := false
	if err := share.UpdateSharing(nil, nil, nil, &textOff, nil, false); err != nil {
		t.Fatal(err)
	}
	probedAfterStop, err := consumer.Probe(t.Context(), consumer.Config().Nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(probedAfterStop.Capabilities, computeCapabilityTextInference) || len(probedAfterStop.TextModels) != 0 {
		t.Fatalf("expected text_inference cleared after stop, got %+v", probedAfterStop)
	}
}
