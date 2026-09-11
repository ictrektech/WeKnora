-- Keep the product surface that owns a conversation explicit. Existing and
-- newly-created ordinary chats remain in the platform workspace.
ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS workspace_mode VARCHAR(32) NOT NULL DEFAULT 'platform';

CREATE INDEX IF NOT EXISTS idx_sessions_tenant_workspace_mode
    ON sessions (tenant_id, workspace_mode);
