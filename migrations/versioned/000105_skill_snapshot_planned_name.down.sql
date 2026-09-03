DO $$ BEGIN RAISE NOTICE '[Migration 000105 down] Dropping planned_name from tenant_skill_snapshots'; END $$;

ALTER TABLE IF EXISTS tenant_skill_snapshots
    DROP COLUMN IF EXISTS planned_name;
