-- Migration 000106: add the tenant-level legal workspace access switch.
-- The enabled default preserves contract-review behavior for existing tenants.
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS legal_workspace_config JSONB NOT NULL
    DEFAULT '{"enabled": true}'::JSONB;

COMMENT ON COLUMN tenants.legal_workspace_config IS
    'Tenant-level legal workspace access switch. Disabling blocks entry and contract-review APIs but preserves data.';
