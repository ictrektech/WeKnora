-- Smart Archive persistence. This migration is intentionally self-contained:
-- earlier WeKnora versions do not contain the LexAI archive tables, and the
-- retired asset model is not part of this schema.

CREATE TABLE IF NOT EXISTS archive_settings (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL UNIQUE,
    managed_knowledge_base_id VARCHAR(36) NOT NULL DEFAULT '',
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    extraction_model_id VARCHAR(128) NOT NULL DEFAULT '',
    extraction_version VARCHAR(32) NOT NULL DEFAULT '1.0',
    trash_retention_days INTEGER NOT NULL DEFAULT 30,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS archive_import_batches (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    total INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(24) NOT NULL DEFAULT 'processing',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_import_batches_tenant
    ON archive_import_batches(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_archive_import_batches_status
    ON archive_import_batches(tenant_id, status, updated_at);

CREATE TABLE IF NOT EXISTS archive_customers (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(512) NOT NULL,
    normalized VARCHAR(512) NOT NULL,
    aliases JSONB NOT NULL DEFAULT '[]'::JSONB,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_customers_normalized
    ON archive_customers(tenant_id, normalized);

CREATE TABLE IF NOT EXISTS archive_documents (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    import_batch_id VARCHAR(36) NULL REFERENCES archive_import_batches(id) ON DELETE SET NULL,
    knowledge_id VARCHAR(36) NOT NULL DEFAULT '',
    title VARCHAR(512) NOT NULL,
    file_name VARCHAR(1024) NOT NULL,
    file_type VARCHAR(16) NOT NULL,
    file_size BIGINT NOT NULL DEFAULT 0,
    file_hash VARCHAR(64) NOT NULL,
    file_path TEXT NOT NULL,
    document_type VARCHAR(32) NOT NULL DEFAULT 'other',
    business_type VARCHAR(16) NOT NULL DEFAULT 'other',
    customer_id VARCHAR(36) NULL,
    agreement_number VARCHAR(256) NOT NULL DEFAULT '',
    signed_at TIMESTAMP NULL,
    effective_at TIMESTAMP NULL,
    expires_at TIMESTAMP NULL,
    returned_at TIMESTAMP NULL,
    renewed_at TIMESTAMP NULL,
    amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    currency VARCHAR(16) NOT NULL DEFAULT '',
    extracted_text TEXT NOT NULL DEFAULT '',
    extracted_fields JSONB NOT NULL DEFAULT '{}'::JSONB,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    extraction_status VARCHAR(24) NOT NULL DEFAULT 'uploading',
    extraction_version VARCHAR(32) NOT NULL DEFAULT '1.0',
    error_message TEXT NOT NULL DEFAULT '',
    archived_at TIMESTAMP NULL,
    trashed_at TIMESTAMP NULL,
    created_by VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_documents_fingerprint
    ON archive_documents(tenant_id, file_hash, extraction_version)
    WHERE trashed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_archive_documents_listing
    ON archive_documents(tenant_id, trashed_at, archived_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_archive_documents_agreement
    ON archive_documents(tenant_id, agreement_number);

-- Durable import queue. The fingerprint is tenant + file hash + extraction
-- version, allowing retries to reuse one row while parser upgrades create a
-- deliberate new work item for the same source bytes.
CREATE TABLE IF NOT EXISTS archive_import_items (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    -- Empty for an operator-triggered retry that intentionally runs outside
    -- the original import batch. Keep the queue row durable even if that
    -- historical batch is removed; the item itself is the retry audit record.
    batch_id VARCHAR(36) NOT NULL,
    document_id VARCHAR(36) NULL REFERENCES archive_documents(id) ON DELETE SET NULL,
    file_name VARCHAR(1024) NOT NULL,
    file_type VARCHAR(16) NOT NULL DEFAULT '',
    mime_type VARCHAR(128) NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    file_hash VARCHAR(64) NOT NULL,
    file_path TEXT NOT NULL DEFAULT '',
    extraction_version VARCHAR(32) NOT NULL DEFAULT '1.0',
    status VARCHAR(24) NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    failure_counted BOOLEAN NOT NULL DEFAULT FALSE,
    run_id VARCHAR(64) NOT NULL DEFAULT '',
    -- NULL marks a terminal failure that has exhausted retries. Queued rows
    -- use the timestamp for delayed retry/recovery; keeping the column
    -- nullable lets startup recovery distinguish those states without a
    -- second sentinel value.
    available_at TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
    claimed_at TIMESTAMP NULL,
    lease_until TIMESTAMP NULL,
    completed_at TIMESTAMP NULL,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_import_items_fingerprint
	ON archive_import_items(tenant_id, file_hash, extraction_version);
CREATE INDEX IF NOT EXISTS idx_archive_import_items_pending
    ON archive_import_items(status, available_at, lease_until, created_at);
CREATE INDEX IF NOT EXISTS idx_archive_import_items_batch
    ON archive_import_items(tenant_id, batch_id, status);

CREATE TABLE IF NOT EXISTS archive_document_links (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    from_document_id VARCHAR(36) NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    to_document_id VARCHAR(36) NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    relation VARCHAR(32) NOT NULL,
    link_status VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_document_links_pair
    ON archive_document_links(tenant_id, from_document_id, to_document_id, relation);
CREATE INDEX IF NOT EXISTS idx_archive_document_links_from
    ON archive_document_links(tenant_id, from_document_id);
CREATE INDEX IF NOT EXISTS idx_archive_document_links_to
    ON archive_document_links(tenant_id, to_document_id);

CREATE TABLE IF NOT EXISTS archive_field_evidence (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    document_id VARCHAR(36) NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    knowledge_id VARCHAR(36) NOT NULL DEFAULT '',
    chunk_id VARCHAR(36) NOT NULL DEFAULT '',
    field_name VARCHAR(128) NOT NULL,
    value TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    quote TEXT NOT NULL DEFAULT '',
    locator_kind VARCHAR(32) NOT NULL DEFAULT 'text',
    locator JSONB NOT NULL DEFAULT '{}'::JSONB,
    source_start INTEGER NOT NULL DEFAULT 0,
    source_end INTEGER NOT NULL DEFAULT 0,
    is_manual BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_field_evidence_document
    ON archive_field_evidence(tenant_id, document_id, field_name);
CREATE INDEX IF NOT EXISTS idx_archive_field_evidence_source
    ON archive_field_evidence(tenant_id, knowledge_id, chunk_id);

CREATE TABLE IF NOT EXISTS archive_reminders (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    document_id VARCHAR(36) NULL REFERENCES archive_documents(id) ON DELETE SET NULL,
    customer_id VARCHAR(36) NULL REFERENCES archive_customers(id) ON DELETE SET NULL,
    assignee_id VARCHAR(64) NOT NULL,
    type VARCHAR(32) NOT NULL,
    title VARCHAR(512) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    rule JSONB NOT NULL DEFAULT '{}'::JSONB,
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    due_at TIMESTAMP NULL,
    snoozed_until TIMESTAMP NULL,
    last_occurrence_at TIMESTAMP NULL,
    created_by VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_reminders_due
    ON archive_reminders(tenant_id, status, due_at);

CREATE TABLE IF NOT EXISTS archive_reminder_occurrences (
    id VARCHAR(36) PRIMARY KEY,
    reminder_id VARCHAR(36) NOT NULL REFERENCES archive_reminders(id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL,
    fingerprint VARCHAR(128) NOT NULL UNIQUE,
    due_at TIMESTAMP NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS archive_notifications (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    reminder_id VARCHAR(36) NULL REFERENCES archive_reminders(id) ON DELETE CASCADE,
    occurrence_id VARCHAR(36) NULL,
    title VARCHAR(512) NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    read_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_notifications_user
    ON archive_notifications(tenant_id, user_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_archive_notifications_occurrence_lookup
    ON archive_notifications(occurrence_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_notifications_occurrence_unique
    ON archive_notifications(occurrence_id)
    WHERE occurrence_id IS NOT NULL AND occurrence_id <> '';

CREATE TABLE IF NOT EXISTS archive_reminder_candidates (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    document_id VARCHAR(36) NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    document_title VARCHAR(512) NOT NULL DEFAULT '',
    customer_id VARCHAR(36) NULL,
    assignee_id VARCHAR(64) NOT NULL DEFAULT '',
    type VARCHAR(32) NOT NULL,
    source_field VARCHAR(128) NOT NULL,
    event_at TIMESTAMP NOT NULL,
    suggested_offset_days INTEGER NOT NULL DEFAULT 0,
    title VARCHAR(512) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    quote TEXT NOT NULL DEFAULT '',
    locator JSONB NOT NULL DEFAULT '{}'::JSONB,
    rule JSONB NOT NULL DEFAULT '{}'::JSONB,
    needs_review BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    reminder_id VARCHAR(36) NULL,
    fingerprint VARCHAR(160) NOT NULL UNIQUE,
    created_by VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_reminder_candidates_scope
    ON archive_reminder_candidates(tenant_id, status, event_at);
CREATE INDEX IF NOT EXISTS idx_archive_reminder_candidates_document
    ON archive_reminder_candidates(tenant_id, document_id);

CREATE TABLE IF NOT EXISTS document_parse_artifacts (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_document_id VARCHAR(36) NOT NULL DEFAULT '',
    file_hash VARCHAR(64) NOT NULL,
    file_name VARCHAR(1024) NOT NULL DEFAULT '',
    file_type VARCHAR(32) NOT NULL DEFAULT '',
    parser_version VARCHAR(64) NOT NULL,
    markdown_content TEXT NOT NULL,
    result JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_document_parse_artifacts_fingerprint
        UNIQUE (tenant_id, file_hash, parser_version)
);
CREATE INDEX IF NOT EXISTS idx_document_parse_artifacts_scope
    ON document_parse_artifacts(tenant_id, parser_version, updated_at);
CREATE INDEX IF NOT EXISTS idx_document_parse_artifacts_source
    ON document_parse_artifacts(tenant_id, source_document_id);
