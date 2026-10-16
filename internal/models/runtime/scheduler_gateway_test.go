package runtime_test

import (
	"net/http"
	"testing"
	"time"

	modelruntime "github.com/Tencent/WeKnora/internal/models/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSchedulerConnectionsUseDynamicPrivateTransport(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	for _, item := range []struct {
		id   string
		kind types.ModelType
		name string
	}{
		{"registered-model", types.ModelTypeKnowledgeQA, "registered/chat"},
		{"hybrag-ollama-qwen35-2b-qa", types.ModelTypeKnowledgeQA, "v-scheduler/ollama-qa/test"},
		{"hybrag-ollama-qwen35-2b-vlm", types.ModelTypeVLLM, "v-scheduler/ollama-qa/test"},
		{"hybrag-ollama-bge-m3-embedding", types.ModelTypeEmbedding, "v-scheduler/ollama-embedding/test"},
		{"hybrag-ollama-bge-reranker-v2-m3-rerank", types.ModelTypeRerank, "v-scheduler/ollama-rerank/test"},
	} {
		resolved, err := modelruntime.Resolve(modelruntime.Ref{Provider: "generic", Model: item.name, BaseURL: "http://v-scheduler-gateway:8080/v1"})
		require.NoError(t, err)
		original := &http.Client{Timeout: 42 * time.Second}
		endpoint, err := resolved.Endpoint(item.kind, modelruntime.Connection{ModelID: item.id, Client: original})
		require.NoError(t, err)
		require.NotSame(t, original, endpoint.Client)
		require.Equal(t, original.Timeout, endpoint.Client.Timeout)
		custom, err := resolved.Endpoint(item.kind, modelruntime.Connection{ModelID: "user-created-model", Client: original})
		require.NoError(t, err)
		require.NotSame(t, original, custom.Client)
		require.Equal(t, original.Timeout, custom.Client.Timeout)
	}
}
