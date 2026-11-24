CREATE TABLE IF NOT EXISTS user_model_desensitizations (
    user_id VARCHAR(36) NOT NULL,
    model_id VARCHAR(64) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ner BOOLEAN NOT NULL DEFAULT FALSE,
    image BOOLEAN NOT NULL DEFAULT FALSE,
    base_url VARCHAR(2048) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_user_model_desensitizations_model
    ON user_model_desensitizations (model_id);
