package router

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

func TestMatchesKnowledgeTypedFiltersTaskType(t *testing.T) {
	payload, err := json.Marshal(knowledgeIDProbe{KnowledgeID: "kid"})
	if err != nil {
		t.Fatal(err)
	}
	if !matchesKnowledge(types.TypeImageMultimodal, payload, "kid") {
		t.Fatal("knowledge task should match")
	}
	if matchesKnowledge(types.TypeKBClone, payload, "kid") {
		t.Fatal("non-knowledge task must not match")
	}
}

func TestKeepKnowledgeTaskRejectsStaleAndDuplicate(t *testing.T) {
	payload, err := json.Marshal(knowledgeIDProbe{KnowledgeID: "kid", Attempt: 2})
	if err != nil {
		t.Fatal(err)
	}
	task := &asynq.TaskInfo{Type: types.TypeImageMultimodal, Payload: payload}
	seen := map[string]struct{}{}
	if !keepKnowledgeTask(task, map[string]int{"kid": 2}, seen) {
		t.Fatal("current attempt must be kept")
	}
	if keepKnowledgeTask(task, map[string]int{"kid": 2}, seen) {
		t.Fatal("exact duplicate must be removed")
	}
	if keepKnowledgeTask(task, map[string]int{"kid": 3}, map[string]struct{}{}) {
		t.Fatal("stale attempt must be removed")
	}
}

func TestKeepKnowledgeTaskCoalescesWikiTriggers(t *testing.T) {
	task := &asynq.TaskInfo{Type: types.TypeWikiIngest, Payload: []byte(`{"knowledge_base_id":"kb"}`)}
	seen := map[string]struct{}{}
	if !keepKnowledgeTask(task, nil, seen) || keepKnowledgeTask(task, nil, seen) {
		t.Fatal("identical wiki triggers must be coalesced")
	}
}
