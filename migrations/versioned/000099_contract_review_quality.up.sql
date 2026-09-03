-- Add quality, provenance, and evidence fields without changing the legacy
-- contract-review JSON columns. Existing rows are intentionally marked
-- legacy; new service-created rows use pending until a run is finalized.

ALTER TABLE contract_reviews
    ADD COLUMN IF NOT EXISTS analysis_run_id VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS config_hash VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_revision VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_text_hash VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_hash VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS locator JSONB NOT NULL DEFAULT '{}'::JSONB,
    ADD COLUMN IF NOT EXISTS quality_status VARCHAR(16) NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS warnings JSONB NOT NULL DEFAULT '[]'::JSONB;

CREATE INDEX IF NOT EXISTS idx_contract_reviews_analysis_run
    ON contract_reviews(analysis_run_id);

ALTER TABLE contract_review_clauses
    ADD COLUMN IF NOT EXISTS evidence_id VARCHAR(128) NOT NULL DEFAULT '';

ALTER TABLE contract_review_issues
    ADD COLUMN IF NOT EXISTS category VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS finding_type VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS evidence_refs JSONB NOT NULL DEFAULT '[]'::JSONB;
