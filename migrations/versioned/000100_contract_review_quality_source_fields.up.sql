-- Repair deployments where 000099 was recorded as applied before the source
-- provenance fields were present in the migration file. Keep this additive
-- and idempotent so it is safe for both repaired and fresh databases.

ALTER TABLE contract_reviews
    ADD COLUMN IF NOT EXISTS source_revision VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_text_hash VARCHAR(64) NOT NULL DEFAULT '';
