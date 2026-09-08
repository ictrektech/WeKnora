-- Migration 000106 down: remove the tenant-level legal workspace switch.
ALTER TABLE tenants
    DROP COLUMN IF EXISTS legal_workspace_config;
