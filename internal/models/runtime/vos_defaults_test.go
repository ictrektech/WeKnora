package runtime_test

import (
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/api/openaicompletions"
	modelruntime "github.com/Tencent/WeKnora/internal/models/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Check the shipped endpoint and generated request together: a valid thinking
// dialect alone does not prove that the selected backend accepts it.
func TestVOSOllamaDefaultsUseNativeThinkingControl(t *testing.T) {
	script, err := os.ReadFile("../../../scripts/docker-entrypoint.sh")
	require.NoError(t, err)
	_, body, found := strings.Cut(string(script), "\nbuiltin_models:\n")
	require.True(t, found)
	body, _, found = strings.Cut(body, "\nEOF\n")
	require.True(t, found)
	var config struct {
		Models []types.BuiltinModelEntry `yaml:"builtin_models"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("builtin_models:\n"+body), &config))
	checked := 0
	for _, model := range config.Models {
		if model.Type != types.ModelTypeKnowledgeQA && model.Type != types.ModelTypeVLLM {
			continue
		}
		checked++
		t.Run(model.ID, func(t *testing.T) {
			require.Equal(t, "http://model-hub-ollama-qa:11434/v1", model.Parameters.BaseURL)
			resolved, err := modelruntime.Resolve(modelruntime.Ref{
				Provider: model.Parameters.Provider, Model: model.Name, BaseURL: model.Parameters.BaseURL,
				ModelType: model.Type, Extra: model.Parameters.ExtraConfig,
			})
			require.NoError(t, err)
			client := openaicompletions.New(openaicompletions.Config{
				Endpoint: api.Endpoint{Model: model.Name}, Settings: resolved.OpenAICompletions,
				ThinkingLevels: resolved.ThinkingLevels,
			})
			for _, stream := range []bool{false, true} {
				off := false
				request, err := client.BuildRequestBody([]api.Message{{Role: "user", Content: "Generate questions"}}, &api.Options{Thinking: &off}, stream)
				require.NoError(t, err)
				require.Equal(t, "none", request["reasoning_effort"])
				require.NotContains(t, request, "think")
				require.NotContains(t, request, "chat_template_kwargs")
			}
		})
	}
	require.Equal(t, 2, checked)
}
