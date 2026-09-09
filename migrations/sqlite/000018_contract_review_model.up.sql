-- Mirrors migrations/versioned/000102_contract_review_model.up.sql.
-- Empty model_id preserves the legacy Agent/default-model resolution.

ALTER TABLE contract_reviews ADD COLUMN model_id TEXT NOT NULL DEFAULT '';
