-- Description: Bind tenant API keys to an optional user owner.
-- Empty owner_user_id means an existing workspace-level key managed by Owners.

ALTER TABLE tenant_api_keys
    ADD COLUMN IF NOT EXISTS owner_user_id VARCHAR(36) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_tenant_api_keys_owner_user
    ON tenant_api_keys(tenant_id, owner_user_id)
    WHERE revoked_at IS NULL;

COMMENT ON COLUMN tenant_api_keys.owner_user_id IS
    'User that owns a personal external API key; empty for workspace-level keys';
