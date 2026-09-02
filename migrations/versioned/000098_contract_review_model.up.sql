-- Allow each contract review to pin the chat model used by its async worker.
-- Empty model_id preserves the legacy Agent/default-model resolution.

ALTER TABLE contract_reviews
    ADD COLUMN IF NOT EXISTS model_id VARCHAR(64) NOT NULL DEFAULT '';

COMMENT ON COLUMN contract_reviews.model_id IS
    'Optional KnowledgeQA model ID selected for this contract review; empty uses the configured fallback';
