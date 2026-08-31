package rerank

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestOllamaRerankerUsesEmbedEndpointAndSortsScores(t *testing.T) {
	var receivedPath string
	var receivedRequest ollamaEmbedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.2],[3.0],[-3.0]]}`))
	}))
	defer server.Close()

	reranker, err := NewReranker(&RerankerConfig{
		BaseURL:   server.URL + "/v1",
		ModelName: "dengcao/bge-reranker-v2-m3",
		Source:    types.ModelSourceLocal,
	})
	if err != nil {
		t.Fatalf("NewReranker: %v", err)
	}

	results, err := reranker.Rerank(t.Context(), "query", []string{"low", "high", "very low"})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	if receivedPath != "/api/embed" {
		t.Fatalf("path = %q, want /api/embed", receivedPath)
	}
	if receivedRequest.Model != "dengcao/bge-reranker-v2-m3" {
		t.Fatalf("model = %q", receivedRequest.Model)
	}
	if len(receivedRequest.Input) != 3 {
		t.Fatalf("got %d input items, want 3", len(receivedRequest.Input))
	}
	if results[0].Index != 1 || results[1].Index != 0 || results[2].Index != 2 {
		t.Fatalf("result order indexes = [%d %d %d], want [1 0 2]",
			results[0].Index, results[1].Index, results[2].Index)
	}
}

func TestOllamaRerankerSelectedByProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %q, want /api/embed", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.8]]}`))
	}))
	defer server.Close()

	reranker, err := NewReranker(&RerankerConfig{
		BaseURL:   server.URL,
		ModelName: "qllama/bge-reranker-v2-m3:q8_0",
		Provider:  "ollama",
		Source:    types.ModelSourceRemote,
	})
	if err != nil {
		t.Fatalf("NewReranker: %v", err)
	}
	if _, err := reranker.Rerank(t.Context(), "query", []string{"doc"}); err != nil {
		t.Fatalf("Rerank: %v", err)
	}
}

func TestOllamaRerankerSupportsCustomInputTemplate(t *testing.T) {
	var receivedRequest ollamaEmbedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.5]]}`))
	}))
	defer server.Close()

	reranker, err := NewOllamaReranker(&RerankerConfig{
		BaseURL:     server.URL,
		ModelName:   "reranker",
		ExtraConfig: map[string]string{"ollama_rerank_template": "{query}</s></s>{document}"},
	})
	if err != nil {
		t.Fatalf("NewOllamaReranker: %v", err)
	}

	if _, err := reranker.Rerank(t.Context(), "Q", []string{"D"}); err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if got, want := receivedRequest.Input[0], "Q</s></s>D"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}
