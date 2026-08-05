ALTER TABLE models
    ADD COLUMN IF NOT EXISTS owner_user_id VARCHAR(36) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_models_owner_user_id
    ON models (tenant_id, owner_user_id)
    WHERE owner_user_id <> '';
