package config

import "testing"

func TestApplyConversationEnvOverrides_MaxCompletionTokens(t *testing.T) {
	t.Setenv("WEKNORA_CONVERSATION_MAX_COMPLETION_TOKENS", "24576")

	cfg := &Config{
		Conversation: &ConversationConfig{
			Summary: &SummaryConfig{MaxCompletionTokens: 2048},
		},
	}

	applyConversationEnvOverrides(cfg)

	if got := cfg.Conversation.Summary.MaxCompletionTokens; got != 24576 {
		t.Fatalf("MaxCompletionTokens = %d, want 24576", got)
	}
}
