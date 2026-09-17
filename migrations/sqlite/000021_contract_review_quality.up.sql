-- Mirrors migrations/versioned/000103_contract_review_quality.up.sql.
-- New rows are initialized by the service; legacy rows use the migration
-- default quality_status=legacy.

ALTER TABLE contract_reviews ADD COLUMN analysis_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE contract_reviews ADD COLUMN config_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE contract_reviews ADD COLUMN source_revision TEXT NOT NULL DEFAULT '';
ALTER TABLE contract_reviews ADD COLUMN source_text_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE contract_reviews ADD COLUMN source_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE contract_reviews ADD COLUMN locator TEXT NOT NULL DEFAULT '{}';
ALTER TABLE contract_reviews ADD COLUMN quality_status TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE contract_reviews ADD COLUMN warnings TEXT NOT NULL DEFAULT '[]';
CREATE INDEX IF NOT EXISTS idx_contract_reviews_analysis_run ON contract_reviews(analysis_run_id);

ALTER TABLE contract_review_clauses ADD COLUMN evidence_id TEXT NOT NULL DEFAULT '';

ALTER TABLE contract_review_issues ADD COLUMN category TEXT NOT NULL DEFAULT '';
ALTER TABLE contract_review_issues ADD COLUMN finding_type TEXT NOT NULL DEFAULT '';
ALTER TABLE contract_review_issues ADD COLUMN evidence_refs TEXT NOT NULL DEFAULT '[]';
