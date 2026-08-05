DROP INDEX IF EXISTS idx_models_owner_user_id;
ALTER TABLE models DROP COLUMN IF EXISTS owner_user_id;
