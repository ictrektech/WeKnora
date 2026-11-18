DROP INDEX IF EXISTS uq_mcp_services_tenant_hub_external;
DROP INDEX IF EXISTS idx_mcp_services_source;
ALTER TABLE mcp_services DROP COLUMN hub_last_seen_at;
ALTER TABLE mcp_services DROP COLUMN hub_available;
ALTER TABLE mcp_services DROP COLUMN external_id;
ALTER TABLE mcp_services DROP COLUMN source;
