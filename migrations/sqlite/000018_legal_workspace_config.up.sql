-- Tenant-level legal workspace access switch. The enabled default preserves
-- existing contract-review behavior for Lite workspaces.
ALTER TABLE tenants
    ADD COLUMN legal_workspace_config TEXT NOT NULL DEFAULT '{"enabled":true}';
