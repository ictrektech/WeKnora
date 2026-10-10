package api_test

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/api/anthropicmessages"
	"github.com/Tencent/WeKnora/internal/models/api/googlegenai"
	"github.com/Tencent/WeKnora/internal/models/api/openaicompletions"
	"github.com/Tencent/WeKnora/internal/models/api/openairesponses"
	"github.com/stretchr/testify/require"
)

func TestExplicitZeroTemperature(t *testing.T) {
	openAI := api.DefaultOpenAICompletions()
	responses := api.DefaultOpenAIResponses()
	anthropic := api.DefaultAnthropicMessages()
	fixed := openAI
	fixedValue := 1.0
	fixed.FixedTemperature = &fixedValue
	unsupported := openAI
	unsupported.SupportsTemperature = false
	unsupportedResponses := responses
	unsupportedResponses.SupportsTemperature = false
	unsupportedAnthropic := anthropic
	unsupportedAnthropic.SupportsTemperature = false
	on := true
	cases := []struct {
		name  string
		build func([]api.Message, *api.Options, bool) (map[string]any, error)
		key   string
		omit  bool
		fixed bool
		think *bool
	}{
		{name: "OpenAI completions", build: openaicompletions.New(openaicompletions.Config{Settings: openAI}).BuildRequestBody},
		{name: "OpenAI responses", build: openairesponses.New(openairesponses.Config{Settings: responses}).BuildRequestBody},
		{name: "Anthropic", build: anthropicmessages.New(anthropicmessages.Config{Settings: anthropic}).BuildRequestBody},
		{name: "Google", build: googlegenai.New(googlegenai.Config{Settings: api.DefaultGoogleGenerativeAI()}).BuildRequestBody, key: "generationConfig"},
		{name: "fixed temperature", build: openaicompletions.New(openaicompletions.Config{Settings: fixed}).BuildRequestBody, fixed: true},
		{name: "unsupported completions", build: openaicompletions.New(openaicompletions.Config{Settings: unsupported}).BuildRequestBody, omit: true},
		{name: "unsupported responses", build: openairesponses.New(openairesponses.Config{Settings: unsupportedResponses}).BuildRequestBody, omit: true},
		{name: "reasoning responses", build: openairesponses.New(openairesponses.Config{Settings: responses, Reasoning: true}).BuildRequestBody, omit: true},
		{name: "unsupported Anthropic", build: anthropicmessages.New(anthropicmessages.Config{Settings: unsupportedAnthropic}).BuildRequestBody, omit: true},
		{name: "Anthropic thinking", build: anthropicmessages.New(anthropicmessages.Config{Settings: anthropic, Reasoning: true}).BuildRequestBody, omit: true, think: &on},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, explicit := range []bool{false, true} {
				body, err := tc.build([]api.Message{{Role: "user", Content: "hi"}}, &api.Options{TemperatureSet: explicit, Thinking: tc.think, MaxTokens: 8}, false)
				require.NoError(t, err)
				if tc.key != "" {
					body = body[tc.key].(map[string]any)
				}
				if tc.omit || (!explicit && !tc.fixed) {
					require.NotContains(t, body, "temperature")
				} else if tc.fixed {
					require.Equal(t, fixedValue, body["temperature"])
				} else {
					require.Equal(t, 0.0, body["temperature"])
				}
			}
		})
	}
}
