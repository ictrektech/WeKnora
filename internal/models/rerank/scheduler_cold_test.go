package rerank

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/schedulergateway"
	"github.com/Tencent/WeKnora/internal/types"
)

// Exercise the production factory and transport, including the gateway wrapper.
// Record the HTTP request's real deadline without waiting 60 seconds.
type deadlineRecorder func(*http.Request) (*http.Response, error)

func (f deadlineRecorder) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSchedulerColdRerankFactoryDeadline(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	t.Setenv("HYBRAG_V_ROUTER_API_KEY", strings.Repeat("a", 64))
	withRerankSSRFWhitelist(t, "v-scheduler")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] == "hold" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":0,"relevance_score":0.9}]}`))
	}))
	defer server.Close()
	previous := sharedRerankHTTPTransport
	transport := previous.(*http.Transport).Clone()
	var remaining time.Duration
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
	}
	sharedRerankHTTPTransport = deadlineRecorder(func(r *http.Request) (*http.Response, error) {
		if deadline, ok := r.Context().Deadline(); ok {
			remaining = time.Until(deadline)
		}
		return transport.RoundTrip(r)
	})
	t.Cleanup(func() { transport.CloseIdleConnections(); sharedRerankHTTPTransport = previous })
	factory := func(spec *types.ModelSpecOverride) Reranker {
		r, err := NewReranker(&RerankerConfig{Provider: "ollama", BaseURL: schedulergateway.Gateway, ModelName: "fixture", Spec: spec})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := factory(nil)
	rows, err := r.Rerank(t.Context(), "query", []string{"document"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("actual factory request failed: %v", err)
	}
	if remaining < 175*time.Second || remaining > 180*time.Second {
		t.Errorf("cold Scheduler request deadline %v; need bounded 180s including queue and load", remaining)
	}
	if got := newRerankHTTPClientFor(schedulergateway.Gateway, 5*time.Second).Timeout; got != 5*time.Second {
		t.Fatalf("explicit timeout changed: %v", got)
	}
	if got := newRerankHTTPClientFor("https://example.com/v1", 0).Timeout; got != 60*time.Second {
		t.Fatalf("external timeout changed: %v", got)
	}
	// A caller's earlier cancellation remains authoritative.
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = r.Rerank(ctx, "hold", []string{"document"})
	if err == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("caller cancellation lost: %v", err)
	}
}
