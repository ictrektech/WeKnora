CREATE TABLE IF NOT EXISTS contract_reviews (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    title VARCHAR(512) NOT NULL,
    title_customized BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    progress INTEGER NOT NULL DEFAULT 0,
    playbook_id VARCHAR(64) NOT NULL DEFAULT 'general-contract-review',
    playbook_version VARCHAR(32) NOT NULL DEFAULT '1.0',
    represented_party VARCHAR(16) NOT NULL DEFAULT 'neutral',
    resource_ref TEXT NOT NULL DEFAULT '',
    file_name VARCHAR(1024) NOT NULL DEFAULT '',
    file_type VARCHAR(16) NOT NULL DEFAULT '',
    mime_type VARCHAR(255) NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    extracted_content TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    overview JSONB NOT NULL DEFAULT '{}'::JSONB,
    error_message TEXT NOT NULL DEFAULT '',
    archived_at TIMESTAMP NULL,
    started_at TIMESTAMP NULL,
    completed_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS idx_contract_reviews_owner ON contract_reviews(tenant_id, user_id, archived_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_contract_reviews_status ON contract_reviews(status);

CREATE TABLE IF NOT EXISTS contract_review_clauses (
    id VARCHAR(36) PRIMARY KEY,
    review_id VARCHAR(36) NOT NULL REFERENCES contract_reviews(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL,
    title VARCHAR(512) NOT NULL,
    excerpt TEXT NOT NULL DEFAULT '',
    source_start INTEGER NOT NULL DEFAULT 0,
    source_end INTEGER NOT NULL DEFAULT 0,
    review_status VARCHAR(16) NOT NULL DEFAULT 'pending',
    issue_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_contract_review_clauses_sequence ON contract_review_clauses(review_id, sequence);

CREATE TABLE IF NOT EXISTS contract_review_issues (
    id VARCHAR(36) PRIMARY KEY,
    review_id VARCHAR(36) NOT NULL REFERENCES contract_reviews(id) ON DELETE CASCADE,
    clause_id VARCHAR(36) NOT NULL REFERENCES contract_review_clauses(id) ON DELETE CASCADE,
    fingerprint VARCHAR(64) NOT NULL UNIQUE,
    sequence INTEGER NOT NULL,
    risk_level VARCHAR(16) NOT NULL,
    title VARCHAR(512) NOT NULL,
    explanation TEXT NOT NULL,
    original_quote TEXT NOT NULL,
    suggestion TEXT NOT NULL,
    source_start INTEGER NOT NULL DEFAULT 0,
    source_end INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_contract_review_issues_review ON contract_review_issues(review_id, sequence);
CREATE INDEX IF NOT EXISTS idx_contract_review_issues_clause ON contract_review_issues(clause_id);
