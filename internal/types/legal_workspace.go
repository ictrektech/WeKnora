package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

// LegalWorkspaceDefaultEnabledEnv controls the default legal workspace state
// for newly created tenants and tenants without a persisted workspace setting.
// Persisted tenant settings always take precedence over this environment value.
const LegalWorkspaceDefaultEnabledEnv = "ICTREK_LEGAL_WORKSPACE_DEFAULT_ENABLED"

// LegalWorkspaceConfig controls access to the tenant's legal workspace.
// A nil config follows the deployment default so operators can choose the
// initial state for newly created or otherwise unconfigured tenants.
type LegalWorkspaceConfig struct {
	Enabled bool `json:"enabled"`
}

// DefaultLegalWorkspaceEnabled returns the deployment default for the legal
// workspace. The default is disabled; an explicit true/false environment value
// overrides it. Invalid values fail closed to disabled.
func DefaultLegalWorkspaceEnabled() bool {
	value := strings.TrimSpace(os.Getenv(LegalWorkspaceDefaultEnabledEnv))
	if value == "" {
		return false
	}
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

// DefaultLegalWorkspaceConfig returns the deployment-configured default.
func DefaultLegalWorkspaceConfig() *LegalWorkspaceConfig {
	return &LegalWorkspaceConfig{Enabled: DefaultLegalWorkspaceEnabled()}
}

// EffectiveLegalWorkspaceConfig normalizes a possibly absent config.
func EffectiveLegalWorkspaceConfig(cfg *LegalWorkspaceConfig) *LegalWorkspaceConfig {
	if cfg == nil {
		return DefaultLegalWorkspaceConfig()
	}
	return cfg
}

// IsEnabled reports whether legal workspace access is allowed.
func (c *LegalWorkspaceConfig) IsEnabled() bool {
	return EffectiveLegalWorkspaceConfig(c).Enabled
}

// Value implements driver.Valuer for the tenant JSON configuration column.
func (c LegalWorkspaceConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner and accepts both PostgreSQL []byte and SQLite
// string values returned for JSON columns.
func (c *LegalWorkspaceConfig) Scan(value interface{}) error {
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
		return errors.New("legal workspace config: expected JSON bytes or string")
	}
	return json.Unmarshal(raw, c)
}

// ContractReviewResource identifies a source object owned by one contract
// review. The repository returns these before deleting review rows so the
// service can clean physical storage after the database transaction commits.
type ContractReviewResource struct {
	ReviewID  string
	Reference string
}
