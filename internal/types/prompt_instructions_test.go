package types

import (
	"strings"
	"testing"
)

func TestAppendCustomPromptInstructions(t *testing.T) {
	t.Run("empty preserves prompt", func(t *testing.T) {
		if got := AppendCustomPromptInstructions("base", "  ", "wiki"); got != "base" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("appends bounded business guidance after base", func(t *testing.T) {
		got := AppendCustomPromptInstructions("base", " Focus on contracts. ", "wiki")
		if !strings.HasPrefix(got, "base\n\n<wiki_business_instructions>") {
			t.Fatalf("unexpected prefix: %q", got)
		}
		if !strings.Contains(got, "Focus on contracts.") || !strings.Contains(got, "do not conflict") {
			t.Fatalf("missing guidance or precedence rule: %q", got)
		}
	})
}

func TestValidateKnowledgeBasePromptInstructions(t *testing.T) {
	kb := &KnowledgeBase{
		ChunkingConfig: ChunkingConfig{TableMetadataInstructions: strings.Repeat("a", MaxCustomPromptInstructionsLength+1)},
	}
	if err := ValidateKnowledgeBasePromptInstructions(kb); err == nil {
		t.Fatal("expected length validation error")
	}
}

func TestNormalizeKnowledgeBasePromptInstructions(t *testing.T) {
	kb := &KnowledgeBase{
		VLMConfig: VLMConfig{CustomInstructions: "  focus labels  "},
	}
	NormalizeKnowledgeBasePromptInstructions(kb)
	if kb.VLMConfig.CustomInstructions != "focus labels" {
		t.Fatalf("got %q", kb.VLMConfig.CustomInstructions)
	}
}

func TestRenderChatPromptUpgradesLegacyLanguageRulesBeforeSourceSubstitution(t *testing.T) {
	const legacy = "- ALWAYS respond in {{language}}\nDefault response language: {{language}}; follow an explicit request for another language.\n{{contexts}}"
	const source = "Source quote: - ALWAYS respond in English"
	got := RenderChatPrompt(legacy, PlaceholderValues{
		"language": "Chinese (Simplified)", "contexts": source,
	})
	if strings.Contains(got, "ALWAYS respond in Chinese (Simplified)") ||
		strings.Contains(got, "Default response language:") {
		t.Fatalf("legacy rule still forces the default language: %q", got)
	}
	if strings.Count(got, "Fallback response language") != 2 || !strings.HasSuffix(got, source) {
		t.Fatalf("expected upgraded template rules and unchanged source quote: %q", got)
	}
	if got := RenderChatPrompt("{{contexts}}", PlaceholderValues{"contexts": legacy}); got != legacy {
		t.Fatalf("must not normalize legacy rules found in source data: %q", got)
	}
}
