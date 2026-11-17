package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVNotePublicIdentityIsStableAcrossGenerations(t *testing.T) {
	logical := VNoteLogicalKnowledgeID("kb", "note")
	for _, physical := range []string{"generation-1", "generation-2"} {
		k := Knowledge{ID: physical, KnowledgeBaseID: "kb", Channel: "vnote", Metadata: []byte(`{"vnote_note_id":"note"}`)}
		encoded, err := json.Marshal(k)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(encoded, &body))
		require.Equal(t, logical, body["id"])
		require.Equal(t, physical, k.ID, "serialization must not mutate internal identity")
		result := SearchResult{KnowledgeID: physical, KnowledgeBaseID: "kb", KnowledgeChannel: "vnote", Metadata: map[string]string{"vnote_note_id": "note"}}
		encoded, err = json.Marshal(result)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encoded, &body))
		require.Equal(t, logical, body["knowledge_id"])
	}
}
