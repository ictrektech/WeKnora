DROP INDEX IF EXISTS uq_mcp_services_tenant_hub_external;
DROP INDEX IF EXISTS idx_mcp_services_source;

ALTER TABLE mcp_services
    DROP COLUMN IF EXISTS hub_last_seen_at,
    DROP COLUMN IF EXISTS hub_available,
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS source;
