CREATE TABLE IF NOT EXISTS contract_reviews (
    id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, user_id TEXT NOT NULL, title TEXT NOT NULL,
    title_customized INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'draft', progress INTEGER NOT NULL DEFAULT 0,
    playbook_id TEXT NOT NULL DEFAULT 'general-contract-review', playbook_version TEXT NOT NULL DEFAULT '1.0', represented_party TEXT NOT NULL DEFAULT 'neutral',
    resource_ref TEXT NOT NULL DEFAULT '', file_name TEXT NOT NULL DEFAULT '', file_type TEXT NOT NULL DEFAULT '', mime_type TEXT NOT NULL DEFAULT '', file_size INTEGER NOT NULL DEFAULT 0,
    extracted_content TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL DEFAULT '{}', overview TEXT NOT NULL DEFAULT '{}', error_message TEXT NOT NULL DEFAULT '',
    archived_at DATETIME, started_at DATETIME, completed_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, deleted_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_contract_reviews_owner ON contract_reviews(tenant_id, user_id, archived_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_contract_reviews_status ON contract_reviews(status);
CREATE TABLE IF NOT EXISTS contract_review_clauses (
    id TEXT PRIMARY KEY, review_id TEXT NOT NULL REFERENCES contract_reviews(id) ON DELETE CASCADE, sequence INTEGER NOT NULL, title TEXT NOT NULL, excerpt TEXT NOT NULL DEFAULT '',
    source_start INTEGER NOT NULL DEFAULT 0, source_end INTEGER NOT NULL DEFAULT 0, review_status TEXT NOT NULL DEFAULT 'pending', issue_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_contract_review_clauses_sequence ON contract_review_clauses(review_id, sequence);
CREATE TABLE IF NOT EXISTS contract_review_issues (
    id TEXT PRIMARY KEY, review_id TEXT NOT NULL REFERENCES contract_reviews(id) ON DELETE CASCADE, clause_id TEXT NOT NULL REFERENCES contract_review_clauses(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL UNIQUE, sequence INTEGER NOT NULL, risk_level TEXT NOT NULL, title TEXT NOT NULL, explanation TEXT NOT NULL, original_quote TEXT NOT NULL, suggestion TEXT NOT NULL,
    source_start INTEGER NOT NULL DEFAULT 0, source_end INTEGER NOT NULL DEFAULT 0, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_contract_review_issues_review ON contract_review_issues(review_id, sequence);
CREATE INDEX IF NOT EXISTS idx_contract_review_issues_clause ON contract_review_issues(clause_id);
