ALTER TABLE mcp_services ADD COLUMN source VARCHAR(32) NOT NULL DEFAULT 'manual';
ALTER TABLE mcp_services ADD COLUMN external_id VARCHAR(255);
ALTER TABLE mcp_services ADD COLUMN hub_available BOOLEAN NOT NULL DEFAULT 1;
ALTER TABLE mcp_services ADD COLUMN hub_last_seen_at DATETIME;

CREATE INDEX IF NOT EXISTS idx_mcp_services_source ON mcp_services(source);
CREATE UNIQUE INDEX IF NOT EXISTS uq_mcp_services_tenant_hub_external
    ON mcp_services(tenant_id, external_id)
    WHERE source = 'mcphub' AND external_id IS NOT NULL AND deleted_at IS NULL;
