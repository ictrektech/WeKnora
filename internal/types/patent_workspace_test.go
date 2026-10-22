package types

import "testing"

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
