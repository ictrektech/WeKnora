-- Migration: 000075_tenant_api_key_scope_repair
-- Description: Repair databases whose migration record advanced past 000071
-- without applying the platform API key scope columns and constraints.

DO $$ BEGIN RAISE NOTICE '[Migration 000075] Repairing tenant API key scope columns...'; END $$;

ALTER TABLE tenant_api_keys
    ADD COLUMN IF NOT EXISTS scope_type VARCHAR(16) NOT NULL DEFAULT 'tenant';

ALTER TABLE tenant_api_keys
    ALTER COLUMN tenant_id DROP NOT NULL;

ALTER TABLE tenant_api_keys
    DROP CONSTRAINT IF EXISTS chk_tenant_api_keys_scope;

ALTER TABLE tenant_api_keys
    ADD CONSTRAINT chk_tenant_api_keys_scope CHECK (
        (scope_type = 'tenant' AND tenant_id IS NOT NULL)
        OR (scope_type = 'platform' AND tenant_id IS NULL AND full_access = FALSE)
    );

CREATE INDEX IF NOT EXISTS idx_tenant_api_keys_scope_type
    ON tenant_api_keys(scope_type);

DO $$ BEGIN RAISE NOTICE '[Migration 000075] Tenant API key scope columns ready'; END $$;
