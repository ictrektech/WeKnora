ALTER TABLE mcp_services
    ADD COLUMN IF NOT EXISTS source VARCHAR(32) NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS external_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS hub_available BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS hub_last_seen_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_mcp_services_source ON mcp_services(source);
CREATE UNIQUE INDEX IF NOT EXISTS uq_mcp_services_tenant_hub_external
    ON mcp_services(tenant_id, external_id)
    WHERE source = 'mcphub' AND external_id IS NOT NULL AND deleted_at IS NULL;
