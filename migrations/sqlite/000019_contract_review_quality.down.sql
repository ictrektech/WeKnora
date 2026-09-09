DROP INDEX IF EXISTS idx_contract_reviews_analysis_run;

-- SQLite versions used by WeKnora support DROP COLUMN. Keep the down
-- migration explicit so local migration tooling can reverse this additive
-- change where the engine permits it.
ALTER TABLE contract_review_issues DROP COLUMN evidence_refs;
ALTER TABLE contract_review_issues DROP COLUMN finding_type;
ALTER TABLE contract_review_issues DROP COLUMN category;
ALTER TABLE contract_review_clauses DROP COLUMN evidence_id;
ALTER TABLE contract_reviews DROP COLUMN warnings;
ALTER TABLE contract_reviews DROP COLUMN quality_status;
ALTER TABLE contract_reviews DROP COLUMN locator;
ALTER TABLE contract_reviews DROP COLUMN source_hash;
ALTER TABLE contract_reviews DROP COLUMN source_text_hash;
ALTER TABLE contract_reviews DROP COLUMN source_revision;
ALTER TABLE contract_reviews DROP COLUMN config_hash;
ALTER TABLE contract_reviews DROP COLUMN analysis_run_id;
