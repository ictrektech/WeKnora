CREATE TABLE platform_user_identities (
    platform_id VARCHAR(255) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    user_id VARCHAR(128) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (platform_id, subject),
    UNIQUE (platform_id, user_id)
);
