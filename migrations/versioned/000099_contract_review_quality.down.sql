DROP INDEX IF EXISTS idx_contract_reviews_analysis_run;

ALTER TABLE contract_review_issues
    DROP COLUMN IF EXISTS evidence_refs,
    DROP COLUMN IF EXISTS finding_type,
    DROP COLUMN IF EXISTS category;

ALTER TABLE contract_review_clauses
    DROP COLUMN IF EXISTS evidence_id;

ALTER TABLE contract_reviews
    DROP COLUMN IF EXISTS warnings,
    DROP COLUMN IF EXISTS quality_status,
    DROP COLUMN IF EXISTS locator,
    DROP COLUMN IF EXISTS source_hash,
    DROP COLUMN IF EXISTS source_text_hash,
    DROP COLUMN IF EXISTS source_revision,
    DROP COLUMN IF EXISTS config_hash,
    DROP COLUMN IF EXISTS analysis_run_id;
