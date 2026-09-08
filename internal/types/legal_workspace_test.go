package types

import "testing"

func TestLegalWorkspaceConfigDefaultsEnabled(t *testing.T) {
	var cfg *LegalWorkspaceConfig
	if !cfg.IsEnabled() {
		t.Fatal("nil legal workspace config must remain enabled")
	}
	if !DefaultLegalWorkspaceConfig().IsEnabled() {
		t.Fatal("default legal workspace config must be enabled")
	}
}

func TestLegalWorkspaceConfigScanAndValue(t *testing.T) {
	var cfg LegalWorkspaceConfig
	if err := cfg.Scan(`{"enabled":false}`); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if cfg.IsEnabled() {
		t.Fatal("scanned disabled config must remain disabled")
	}
	value, err := cfg.Value()
	if err != nil || string(value.([]byte)) != `{"enabled":false}` {
		t.Fatalf("Value() = %v, %v", value, err)
	}
}
