package types

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLegalAssistantBuiltinConfigIsEvidenceFirst(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "config", "builtin_agents.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg builtinAgentsFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	var found *BuiltinAgentEntry
	for i := range cfg.BuiltinAgents {
		if cfg.BuiltinAgents[i].ID == BuiltinLegalAssistantID {
			found = &cfg.BuiltinAgents[i]
			break
		}
	}
	if found == nil {
		t.Fatal("builtin legal assistant is missing from builtin_agents.yaml")
	}
	if found.Config.SystemPromptID != "legal_assistant_main" {
		t.Fatalf("system prompt id = %q", found.Config.SystemPromptID)
	}
	if !found.Config.WebSearchEnabled || len(found.Config.AllowedTools) == 0 {
		t.Fatal("legal assistant must have optional web search and read-only retrieval tools configured")
	}
	prompt, err := os.ReadFile(filepath.Join(root, "config", "prompt_templates", "agent_system_prompt.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsPromptID(prompt, "legal_assistant_main") {
		t.Fatal("legal_assistant_main prompt is missing")
	}
}

func containsPromptID(data []byte, id string) bool {
	var file struct {
		Templates []struct {
			ID string `yaml:"id"`
		} `yaml:"templates"`
	}
	if yaml.Unmarshal(data, &file) != nil {
		return false
	}
	for _, template := range file.Templates {
		if template.ID == id {
			return true
		}
	}
	return false
}

func TestApplyBuiltinAgentLocalizationOverlaysYAMLLocale(t *testing.T) {
	restore := OverrideBuiltinAgentEntriesForTest(map[string]*BuiltinAgentEntry{
		BuiltinQuickAnswerID: {
			ID:     BuiltinQuickAnswerID,
			Avatar: "quick.png",
			I18n: map[string]BuiltinAgentI18n{
				"default": {Name: "快速问答", Description: "中文 RAG"},
				"en-US":   {Name: "Quick Answer", Description: "Knowledge base RAG Q&A"},
				"zh-CN":   {Name: "快速问答", Description: "中文 RAG"},
			},
		},
	})
	t.Cleanup(restore)

	agent := &CustomAgent{
		ID:          BuiltinQuickAnswerID,
		Name:        "快速问答",
		Description: "中文 RAG",
		Avatar:      "",
		IsBuiltin:   true,
		TenantID:    1,
	}
	ctx := context.WithValue(context.Background(), LanguageContextKey, "en-US")
	ApplyBuiltinAgentLocalization(ctx, agent)

	if agent.Name != "Quick Answer" {
		t.Fatalf("Name = %q, want Quick Answer", agent.Name)
	}
	if agent.Description != "Knowledge base RAG Q&A" {
		t.Fatalf("Description = %q, want English copy", agent.Description)
	}
	if agent.Avatar != "quick.png" {
		t.Fatalf("Avatar = %q, want quick.png", agent.Avatar)
	}
}

func TestApplyBuiltinAgentLocalizationNilSafe(t *testing.T) {
	ApplyBuiltinAgentLocalization(context.Background(), nil)
}

func TestApplyBuiltinAgentLocalizationLeavesUnknownAgents(t *testing.T) {
	agent := &CustomAgent{ID: "custom-agent-1", Name: "Mine", Description: "keep me"}
	ctx := context.WithValue(context.Background(), LanguageContextKey, "en-US")
	ApplyBuiltinAgentLocalization(ctx, agent)
	if agent.Name != "Mine" || agent.Description != "keep me" {
		t.Fatalf("custom agent was rewritten: %+v", agent)
	}
}
