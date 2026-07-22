-- Migration: 000074_principal_model_repair
-- Description: Repair databases that had already consumed version 000064 before upstream introduced 000064_principal_model.

ALTER TABLE mcp_oauth_tokens
    ADD COLUMN IF NOT EXISTS principal_type VARCHAR(32),
    ADD COLUMN IF NOT EXISTS principal_id VARCHAR(512);

ALTER TABLE mcp_oauth_tokens
    ALTER COLUMN user_id TYPE VARCHAR(512);

UPDATE mcp_oauth_tokens
SET principal_type = 'web_user',
    principal_id = user_id
WHERE (principal_type IS NULL OR principal_type = '')
  AND user_id IS NOT NULL
  AND user_id <> '';

ALTER TABLE mcp_oauth_tokens
    ALTER COLUMN principal_type SET NOT NULL,
    ALTER COLUMN principal_id SET NOT NULL;

DROP INDEX IF EXISTS idx_mcp_oauth_tokens_tenant_user_svc;

CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_oauth_tokens_tenant_principal_svc
    ON mcp_oauth_tokens(tenant_id, principal_type, principal_id, service_id);

CREATE INDEX IF NOT EXISTS idx_mcp_oauth_tokens_principal
    ON mcp_oauth_tokens(principal_type, principal_id);

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS api_principal_config JSONB;

ALTER TABLE sessions
    ALTER COLUMN user_id TYPE VARCHAR(512);
