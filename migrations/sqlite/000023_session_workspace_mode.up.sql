-- Existing SQLite sessions are ordinary platform chats by default.
ALTER TABLE sessions ADD COLUMN workspace_mode TEXT NOT NULL DEFAULT 'platform';
CREATE INDEX IF NOT EXISTS idx_sessions_tenant_workspace_mode
    ON sessions (tenant_id, workspace_mode);
