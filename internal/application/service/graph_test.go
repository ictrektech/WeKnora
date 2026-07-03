package service

import "testing"

func TestGraphLLMConcurrencyCapsAtHalfMainQA(t *testing.T) {
	t.Setenv("WEKNORA_MAIN_QA_MODEL_CONCURRENCY", "4")
	t.Setenv("WEKNORA_GRAPH_LLM_CONCURRENCY", "4")

	if got := graphLLMConcurrency(4); got != 2 {
		t.Fatalf("graphLLMConcurrency() = %d, want 2", got)
	}
}
