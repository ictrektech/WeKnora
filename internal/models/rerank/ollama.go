package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const defaultOllamaRerankTemplate = "Query: {query}\nDocument: {document}"

// OllamaReranker adapts Ollama embed-style reranker models to WeKnora's
// Reranker interface. Models such as dengcao/bge-reranker-v2-m3 are exposed by
// Ollama through /api/embed instead of an OpenAI-compatible /rerank endpoint.
type OllamaReranker struct {
	modelName string
	modelID   string
	baseURL   string
	template  string
	client    *http.Client
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Embedding  []float64   `json:"embedding"`
}

func NewOllamaReranker(config *RerankerConfig) (*OllamaReranker, error) {
	modelName := strings.TrimSpace(config.ModelName)
	if modelName == "" {
		return nil, fmt.Errorf("ollama rerank model name is required")
	}

	baseURL := normalizeOllamaRerankBaseURL(config.BaseURL)
	if baseURL == "" {
		baseURL = normalizeOllamaRerankBaseURL(os.Getenv("OLLAMA_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	template := defaultOllamaRerankTemplate
	if config.ExtraConfig != nil {
		if raw := strings.TrimSpace(config.ExtraConfig["ollama_rerank_template"]); raw != "" {
			template = raw
		}
	}

	return &OllamaReranker{
		modelName: modelName,
		modelID:   config.ModelID,
		baseURL:   baseURL,
		template:  template,
		client:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func normalizeOllamaRerankBaseURL(raw string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	return baseURL
}

func (r *OllamaReranker) Rerank(ctx context.Context, query string, documents []string) ([]RankResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	if len(documents) == 0 {
		return nil, fmt.Errorf("documents cannot be empty")
	}

	inputs := make([]string, 0, len(documents))
	for _, document := range documents {
		inputs = append(inputs, r.renderInput(query, document))
	}

	reqBody := ollamaEmbedRequest{
		Model: r.modelName,
		Input: inputs,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal ollama embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ollama embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ollama embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama embed API error: Http Status: %s", resp.Status)
	}

	var parsed ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode ollama embed response: %w", err)
	}
	if len(parsed.Embeddings) == 0 && len(parsed.Embedding) > 0 {
		parsed.Embeddings = [][]float64{parsed.Embedding}
	}
	if len(parsed.Embeddings) != len(documents) {
		return nil, fmt.Errorf("ollama embed returned %d embeddings for %d documents", len(parsed.Embeddings), len(documents))
	}

	results := make([]RankResult, 0, len(documents))
	for i, embedding := range parsed.Embeddings {
		if len(embedding) == 0 {
			return nil, fmt.Errorf("ollama embed returned empty score embedding for document %d", i)
		}
		results = append(results, RankResult{
			Index:          i,
			Document:       DocumentInfo{Text: documents[i]},
			RelevanceScore: normalizeOllamaRerankScore(embedding[0]),
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})
	return results, nil
}

func (r *OllamaReranker) renderInput(query string, document string) string {
	input := strings.ReplaceAll(r.template, "{query}", query)
	input = strings.ReplaceAll(input, "{document}", document)
	return input
}

func normalizeOllamaRerankScore(score float64) float64 {
	if score >= 0 && score <= 1 {
		return score
	}
	return 1 / (1 + math.Exp(-score))
}

func (r *OllamaReranker) GetModelName() string {
	return r.modelName
}

func (r *OllamaReranker) GetModelID() string {
	return r.modelID
}
