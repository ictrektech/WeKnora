package runtime

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/models/api"
)

// inferAPIFromURL preserves protocol selection for legacy URLs.
func inferAPIFromURL(baseURL string, current api.API) api.API {
	lower := strings.ToLower(strings.TrimRight(baseURL, "/"))
	switch {
	case strings.HasSuffix(lower, "/anthropic") || strings.HasSuffix(lower, "/anthropic/v1"):
		return api.APIAnthropicMessages
	case current == api.APIGoogleGenerativeAI &&
		(strings.HasSuffix(lower, "/openai") || strings.Contains(lower, "/openai/")):
		return api.APIOpenAICompletions
	}
	return current
}

// applyLegacyThinkingControl maps the pre-catalog thinking_control value
// onto the new settings. Unknown non-empty values keep the historical
// fallback (chat_template_kwargs).
func applyLegacyThinkingControl(s *api.OpenAICompletionsSettings, value string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return
	case "none":
		s.ThinkingFormat = api.ThinkingFormatNone
	case "enable_thinking":
		s.ThinkingFormat = api.ThinkingFormatEnableThinking
	case "thinking_type":
		s.ThinkingFormat = api.ThinkingFormatThinkingType
	case "think":
		s.ThinkingFormat = api.ThinkingFormatThink
	case "reasoning_effort":
		s.ThinkingFormat = api.ThinkingFormatReasoningEffort
	default:
		s.ThinkingFormat = api.ThinkingFormatChatTemplateKwargs
	}
	if s.ThinkingFormat == api.ThinkingFormatThink || s.ThinkingFormat == api.ThinkingFormatReasoningEffort {
		// These legacy strategies only encoded an explicitly requested boolean,
		// independently of the provider's default thinking policy.
		s.ThinkingAlwaysSend = false
		s.ThinkingDisableOnNonStream = false
		s.ThinkingBudgetField = ""
		s.SupportsReasoningEffort = false
	}
}
