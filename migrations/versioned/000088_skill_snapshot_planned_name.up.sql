-- Description: The name a snapshot was going to be committed under, recorded
-- before the provider call. snapshot_id can only be filled in afterwards, so a
-- process that died between the commit and that write left a real provider
-- snapshot the ledger could not name — and therefore could never reclaim.
DO $$ BEGIN RAISE NOTICE '[Migration 000088] Adding planned_name to tenant_skill_snapshots'; END $$;

-- tenant_skill_snapshots is created by migration 000094. Keep this shipped
-- migration safe for fresh databases; migration 000105 applies the same
-- column after the table exists.
ALTER TABLE IF EXISTS tenant_skill_snapshots
    ADD COLUMN IF NOT EXISTS planned_name VARCHAR(255);
