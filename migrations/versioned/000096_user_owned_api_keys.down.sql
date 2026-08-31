DROP INDEX IF EXISTS idx_tenant_api_keys_owner_user;

ALTER TABLE tenant_api_keys
    DROP COLUMN IF EXISTS owner_user_id;
