ALTER TABLE user_model_desensitizations
    ADD COLUMN IF NOT EXISTS base_url VARCHAR(2048) NOT NULL DEFAULT '';
