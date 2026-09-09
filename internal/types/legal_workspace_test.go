package types

import "testing"

func TestLegalWorkspaceConfigDefaultFollowsEnvironment(t *testing.T) {
	t.Setenv(LegalWorkspaceDefaultEnabledEnv, "")
	var cfg *LegalWorkspaceConfig
	if cfg.IsEnabled() {
		t.Fatal("nil legal workspace config must default to disabled")
	}
	if DefaultLegalWorkspaceConfig().IsEnabled() {
		t.Fatal("default legal workspace config must be disabled")
	}

	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "false", want: false},
		{value: "true", want: true},
		{value: "TRUE", want: true},
		{value: "invalid", want: false},
	} {
		t.Setenv(LegalWorkspaceDefaultEnabledEnv, test.value)
		if got := DefaultLegalWorkspaceConfig().IsEnabled(); got != test.want {
			t.Fatalf("env %q produced enabled=%v, want %v", test.value, got, test.want)
		}
	}
}

func TestTenantBeforeCreateUsesLegalWorkspaceDefault(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "", want: false},
		{value: "true", want: true},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv(LegalWorkspaceDefaultEnabledEnv, test.value)
			tenant := &Tenant{}
			if err := tenant.BeforeCreate(nil); err != nil {
				t.Fatalf("BeforeCreate: %v", err)
			}
			if tenant.LegalWorkspaceConfig == nil {
				t.Fatal("BeforeCreate must initialize legal workspace config")
			}
			if got := tenant.LegalWorkspaceConfig.Enabled; got != test.want {
				t.Fatalf("enabled=%v, want %v", got, test.want)
			}
		})
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
