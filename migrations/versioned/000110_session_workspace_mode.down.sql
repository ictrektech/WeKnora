DROP INDEX IF EXISTS idx_sessions_tenant_workspace_mode;
ALTER TABLE sessions DROP COLUMN IF EXISTS workspace_mode;
