-- Migration 000107 repairs deployments whose schema_migrations ledger
-- already passed 000106 before the legal workspace column was present.
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS legal_workspace_config JSONB NOT NULL
    DEFAULT '{"enabled": true}'::JSONB;

COMMENT ON COLUMN tenants.legal_workspace_config IS
    'Tenant-level legal workspace access switch. Disabling blocks entry and contract-review APIs but preserves data.';
