DO $$
BEGIN
    IF to_regclass('public.tenant_skill_snapshots') IS NULL THEN
        RAISE NOTICE '[Migration 000088] tenant_skill_snapshots does not exist; nothing to roll back';
        RETURN;
    END IF;

    RAISE NOTICE '[Migration 000088] Dropping planned_name from tenant_skill_snapshots';
    ALTER TABLE tenant_skill_snapshots DROP COLUMN IF EXISTS planned_name;
END $$;
