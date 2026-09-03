-- Migration 000088 introduced planned_name before tenant_skill_snapshots was
-- created by migration 000094. This post-create repair preserves the shipped
-- version while making the column available on fresh databases as well.
DO $$ BEGIN RAISE NOTICE '[Migration 000105] Adding planned_name to tenant_skill_snapshots'; END $$;

ALTER TABLE tenant_skill_snapshots
    ADD COLUMN IF NOT EXISTS planned_name VARCHAR(255);

COMMENT ON COLUMN tenant_skill_snapshots.planned_name IS
    'Name passed to CreateSnapshot, written before the provider call so an abandoned build stays identifiable';
