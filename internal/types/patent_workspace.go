package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

// PatentWorkspaceDefaultEnabledEnv controls the default patent workspace state
// for newly created tenants and tenants without a persisted workspace setting.
// Persisted tenant settings always take precedence over this environment value.
const PatentWorkspaceDefaultEnabledEnv = "ICTREK_PATENT_WORKSPACE_DEFAULT_ENABLED"

// PatentWorkspaceConfig controls access to the tenant's patent workspace.
// A nil config follows the deployment default so operators can choose the
// initial state for newly created or otherwise unconfigured tenants.
type PatentWorkspaceConfig struct {
	Enabled bool `json:"enabled"`
}

// DefaultPatentWorkspaceEnabled returns the deployment default for the patent
// workspace. The default is disabled; an explicit true/false environment value
// overrides it. Invalid values fail closed to disabled.
func DefaultPatentWorkspaceEnabled() bool {
	value := strings.TrimSpace(os.Getenv(PatentWorkspaceDefaultEnabledEnv))
	if value == "" {
		return false
	}
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

// DefaultPatentWorkspaceConfig returns the deployment-configured default.
func DefaultPatentWorkspaceConfig() *PatentWorkspaceConfig {
	return &PatentWorkspaceConfig{Enabled: DefaultPatentWorkspaceEnabled()}
}

// EffectivePatentWorkspaceConfig normalizes a possibly absent config.
func EffectivePatentWorkspaceConfig(cfg *PatentWorkspaceConfig) *PatentWorkspaceConfig {
	if cfg == nil {
		return DefaultPatentWorkspaceConfig()
	}
	return cfg
}

// IsEnabled reports whether patent workspace access is allowed.
func (c *PatentWorkspaceConfig) IsEnabled() bool {
	return EffectivePatentWorkspaceConfig(c).Enabled
}

// Value implements driver.Valuer for the tenant JSON configuration column.
func (c PatentWorkspaceConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner and accepts both PostgreSQL []byte and SQLite
// string values returned for JSON columns.
func (c *PatentWorkspaceConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return errors.New("patent workspace config: expected JSON bytes or string")
	}
	return json.Unmarshal(raw, c)
}
