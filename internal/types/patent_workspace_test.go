package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatentExamplePreferenceSurvivesJSONPersistence(t *testing.T) {
	var missing *PatentWorkspaceConfig
	require.True(t, missing.AreExamplesEnabled())
	for _, raw := range []string{`{"enabled":true}`, `{"enabled":false}`} {
		var config PatentWorkspaceConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		require.True(t, config.AreExamplesEnabled(), "legacy config defaults to showing examples, not enabling the workspace")
	}
	for _, value := range []bool{false, true} {
		config := PatentWorkspaceConfig{Enabled: true, ExamplesEnabled: &value}
		raw, err := config.Value()
		require.NoError(t, err)
		for _, storage := range []any{raw, string(raw.([]byte))} {
			var restored PatentWorkspaceConfig
			require.NoError(t, restored.Scan(storage))
			require.Equal(t, value, restored.AreExamplesEnabled())
			restored.Enabled = false
			require.Equal(t, value, restored.AreExamplesEnabled(), "workspace access and example preference are independent")
		}
	}
}

func TestPatentWorkspaceConfigDefaultsAndPersistence(t *testing.T) {
	var missing *PatentWorkspaceConfig
	for _, input := range []struct {
		value string
		want  bool
	}{{"", false}, {"true", true}, {"false", false}, {"bad", false}} {
		t.Setenv(PatentWorkspaceDefaultEnabledEnv, input.value)
		if missing.IsEnabled() != input.want {
			t.Fatalf("default for %q", input.value)
		}
		persisted := &PatentWorkspaceConfig{Enabled: !input.want}
		if persisted.IsEnabled() != !input.want {
			t.Fatal("saved configuration must win")
		}
		var decoded PatentWorkspaceConfig
		raw, err := persisted.Value()
		if err != nil {
			t.Fatal(err)
		}
		if err := decoded.Scan(raw); err != nil || decoded.Enabled != persisted.Enabled {
			t.Fatalf("round trip: %v", err)
		}
	}
	if !WorkspaceModePatentSearch.Valid() || !WorkspaceModePatentWriting.Valid() || WorkspaceMode("patent").Valid() {
		t.Fatal("invalid workspace modes")
	}
}
