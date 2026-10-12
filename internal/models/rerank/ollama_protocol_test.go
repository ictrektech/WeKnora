package rerank

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestOllamaUsesSharedRerankProtocol(t *testing.T) {
	withRerankSSRFWhitelist(t, "127.0.0.1")
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved-provider", true: "local-source"}[local], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/rerank" {
					t.Errorf("path = %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if _, ok := body["input"]; ok {
					t.Error("embedding request sent")
				}
				if body["query"] != "query" || body["model"] != "qllama/bge-reranker-v2-m3:q8_0" {
					t.Errorf("unexpected body: %v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`))
			}))
			defer server.Close()
			cfg := &RerankerConfig{ModelName: "qllama/bge-reranker-v2-m3:q8_0", BaseURL: server.URL, Provider: "ollama"}
			if local {
				cfg.Source = types.ModelSourceLocal
				cfg.Provider = ""
			}
			r, err := NewReranker(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := r.(*protocolReranker); !ok {
				t.Fatalf("reranker = %T", r)
			}
			results, err := r.Rerank(t.Context(), "query", []string{"low", "high"})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 2 || results[0].Index != 1 || results[0].RelevanceScore != 0.9 {
				t.Fatalf("results = %+v", results)
			}
			if cfg.Provider == "generic" {
				t.Fatal("mutated caller config")
			}
		})
	}
}
