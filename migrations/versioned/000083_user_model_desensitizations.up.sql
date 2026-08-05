CREATE TABLE IF NOT EXISTS user_model_desensitizations (
    user_id VARCHAR(36) NOT NULL,
    model_id VARCHAR(64) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ner BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_user_model_desensitizations_model
    ON user_model_desensitizations (model_id);
