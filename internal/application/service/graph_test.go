package service

import "testing"

func TestGraphLLMConcurrencyCapsAtHalfMainQA(t *testing.T) {
	t.Setenv("WEKNORA_MAIN_QA_MODEL_CONCURRENCY", "4")
	t.Setenv("WEKNORA_GRAPH_LLM_CONCURRENCY", "4")

	if got := graphLLMConcurrency(4); got != 2 {
		t.Fatalf("graphLLMConcurrency() = %d, want 2", got)
	}
}

func TestBackgroundLLMCapacityReservesChatSlots(t *testing.T) {
	if got := backgroundLLMCapacity(4, 2); got != 2 {
		t.Fatalf("backgroundLLMCapacity(4, 2) = %d, want 2", got)
	}
	if got := backgroundLLMCapacity(2, 2); got != 1 {
		t.Fatalf("backgroundLLMCapacity(2, 2) = %d, want 1", got)
	}
	if got := backgroundLLMCapacity(4, 0); got != 0 {
		t.Fatalf("backgroundLLMCapacity(4, 0) = %d, want 0", got)
	}
}
