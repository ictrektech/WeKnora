package router

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestMatchesKnowledgeTypedFiltersTaskType(t *testing.T) {
	payload, err := json.Marshal(knowledgeIDProbe{KnowledgeID: "kid"})
	if err != nil {
		t.Fatal(err)
	}
	onlyGraph := map[string]struct{}{types.TypeChunkExtract: {}}
	if !matchesKnowledgeTyped(types.TypeChunkExtract, payload, "kid", onlyGraph) {
		t.Fatal("graph task should match")
	}
	if matchesKnowledgeTyped(types.TypeImageMultimodal, payload, "kid", onlyGraph) {
		t.Fatal("multimodal task must not match graph-only cleanup")
	}
}
