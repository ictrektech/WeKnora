DO $$ BEGIN RAISE NOTICE '[Migration 000122 down] Dropping tenant_skills.served'; END $$;

ALTER TABLE tenant_skills DROP COLUMN IF EXISTS served;
