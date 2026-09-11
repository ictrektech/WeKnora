DROP INDEX IF EXISTS idx_sessions_tenant_workspace_mode;
-- SQLite cannot drop a column on all supported versions. The migration is
-- intentionally irreversible; rolling the app back leaves the additive field.
