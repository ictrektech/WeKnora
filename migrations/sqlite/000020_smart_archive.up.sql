-- Smart Archive persistence for SQLite. Kept self-contained because the
-- current WeKnora SQLite head is 19 and has no archive tables.

CREATE TABLE IF NOT EXISTS archive_settings (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL UNIQUE,
    managed_knowledge_base_id TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    extraction_model_id TEXT NOT NULL DEFAULT '',
    extraction_version TEXT NOT NULL DEFAULT '1.0',
    trash_retention_days INTEGER NOT NULL DEFAULT 30,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS archive_import_batches (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    user_id TEXT NOT NULL,
    total INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'processing',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_import_batches_tenant
    ON archive_import_batches(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_archive_import_batches_status
    ON archive_import_batches(tenant_id, status, updated_at);

CREATE TABLE IF NOT EXISTS archive_customers (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    normalized TEXT NOT NULL,
    aliases TEXT NOT NULL DEFAULT '[]',
    notes TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_customers_normalized
    ON archive_customers(tenant_id, normalized);

CREATE TABLE IF NOT EXISTS archive_documents (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    import_batch_id TEXT REFERENCES archive_import_batches(id) ON DELETE SET NULL,
    knowledge_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    file_name TEXT NOT NULL,
    file_type TEXT NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    file_hash TEXT NOT NULL,
    file_path TEXT NOT NULL,
    document_type TEXT NOT NULL DEFAULT 'other',
    business_type TEXT NOT NULL DEFAULT 'other',
    customer_id TEXT,
    agreement_number TEXT NOT NULL DEFAULT '',
    signed_at DATETIME,
    effective_at DATETIME,
    expires_at DATETIME,
    returned_at DATETIME,
    renewed_at DATETIME,
    amount REAL NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT '',
    extracted_text TEXT NOT NULL DEFAULT '',
    extracted_fields TEXT NOT NULL DEFAULT '{}',
    metadata TEXT NOT NULL DEFAULT '{}',
    extraction_status TEXT NOT NULL DEFAULT 'uploading',
    extraction_version TEXT NOT NULL DEFAULT '1.0',
    error_message TEXT NOT NULL DEFAULT '',
    archived_at DATETIME,
    trashed_at DATETIME,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_documents_fingerprint
    ON archive_documents(tenant_id, file_hash, extraction_version)
    WHERE trashed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_archive_documents_listing
    ON archive_documents(tenant_id, trashed_at, archived_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_archive_documents_agreement
    ON archive_documents(tenant_id, agreement_number);

CREATE TABLE IF NOT EXISTS archive_import_items (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    -- Empty for an operator-triggered retry that intentionally runs outside
    -- the original import batch. Keep the queue row durable even if that
    -- historical batch is removed; the item itself is the retry audit record.
    batch_id TEXT NOT NULL,
    document_id TEXT REFERENCES archive_documents(id) ON DELETE SET NULL,
    file_name TEXT NOT NULL,
    file_type TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    file_size INTEGER NOT NULL DEFAULT 0,
    file_hash TEXT NOT NULL,
    file_path TEXT NOT NULL DEFAULT '',
    extraction_version TEXT NOT NULL DEFAULT '1.0',
    status TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    failure_counted INTEGER NOT NULL DEFAULT 0,
    run_id TEXT NOT NULL DEFAULT '',
    -- NULL marks a terminal failure that has exhausted retries. Queued rows
    -- use the timestamp for delayed retry/recovery; keeping the column
    -- nullable lets startup recovery distinguish those states without a
    -- second sentinel value.
    available_at DATETIME NULL DEFAULT CURRENT_TIMESTAMP,
    claimed_at DATETIME,
    lease_until DATETIME,
    completed_at DATETIME,
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_import_items_fingerprint
	ON archive_import_items(tenant_id, file_hash, extraction_version);
CREATE INDEX IF NOT EXISTS idx_archive_import_items_pending
    ON archive_import_items(status, available_at, lease_until, created_at);
CREATE INDEX IF NOT EXISTS idx_archive_import_items_batch
    ON archive_import_items(tenant_id, batch_id, status);

CREATE TABLE IF NOT EXISTS archive_document_links (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    from_document_id TEXT NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    to_document_id TEXT NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    relation TEXT NOT NULL,
    link_status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_document_links_pair
    ON archive_document_links(tenant_id, from_document_id, to_document_id, relation);
CREATE INDEX IF NOT EXISTS idx_archive_document_links_from
    ON archive_document_links(tenant_id, from_document_id);
CREATE INDEX IF NOT EXISTS idx_archive_document_links_to
    ON archive_document_links(tenant_id, to_document_id);

CREATE TABLE IF NOT EXISTS archive_field_evidence (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    document_id TEXT NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    knowledge_id TEXT NOT NULL DEFAULT '',
    chunk_id TEXT NOT NULL DEFAULT '',
    field_name TEXT NOT NULL,
    value TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 0,
    quote TEXT NOT NULL DEFAULT '',
    locator_kind TEXT NOT NULL DEFAULT 'text',
    locator TEXT NOT NULL DEFAULT '{}',
    source_start INTEGER NOT NULL DEFAULT 0,
    source_end INTEGER NOT NULL DEFAULT 0,
    is_manual INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_field_evidence_document
    ON archive_field_evidence(tenant_id, document_id, field_name);
CREATE INDEX IF NOT EXISTS idx_archive_field_evidence_source
    ON archive_field_evidence(tenant_id, knowledge_id, chunk_id);

CREATE TABLE IF NOT EXISTS archive_reminders (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    document_id TEXT REFERENCES archive_documents(id) ON DELETE SET NULL,
    customer_id TEXT REFERENCES archive_customers(id) ON DELETE SET NULL,
    assignee_id TEXT NOT NULL,
    type TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    rule TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft',
    confidence REAL NOT NULL DEFAULT 0,
    due_at DATETIME,
    snoozed_until DATETIME,
    last_occurrence_at DATETIME,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_reminders_due
    ON archive_reminders(tenant_id, status, due_at);

CREATE TABLE IF NOT EXISTS archive_reminder_occurrences (
    id TEXT PRIMARY KEY,
    reminder_id TEXT NOT NULL REFERENCES archive_reminders(id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    due_at DATETIME NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS archive_notifications (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    user_id TEXT NOT NULL,
    reminder_id TEXT REFERENCES archive_reminders(id) ON DELETE CASCADE,
    occurrence_id TEXT,
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    read_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_notifications_user
    ON archive_notifications(tenant_id, user_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_archive_notifications_occurrence_lookup
    ON archive_notifications(occurrence_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_notifications_occurrence_unique
    ON archive_notifications(occurrence_id)
    WHERE occurrence_id IS NOT NULL AND occurrence_id <> '';

CREATE TABLE IF NOT EXISTS archive_reminder_candidates (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    document_id TEXT NOT NULL REFERENCES archive_documents(id) ON DELETE CASCADE,
    document_title TEXT NOT NULL DEFAULT '',
    customer_id TEXT,
    assignee_id TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    source_field TEXT NOT NULL,
    event_at DATETIME NOT NULL,
    suggested_offset_days INTEGER NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    quote TEXT NOT NULL DEFAULT '',
    locator TEXT NOT NULL DEFAULT '{}',
    rule TEXT NOT NULL DEFAULT '{}',
    needs_review INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    reminder_id TEXT,
    fingerprint TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_archive_reminder_candidates_scope
    ON archive_reminder_candidates(tenant_id, status, event_at);
CREATE INDEX IF NOT EXISTS idx_archive_reminder_candidates_document
    ON archive_reminder_candidates(tenant_id, document_id);

CREATE TABLE IF NOT EXISTS document_parse_artifacts (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    source_document_id TEXT NOT NULL DEFAULT '',
    file_hash TEXT NOT NULL,
    file_name TEXT NOT NULL DEFAULT '',
    file_type TEXT NOT NULL DEFAULT '',
    parser_version TEXT NOT NULL,
    markdown_content TEXT NOT NULL,
    result TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, file_hash, parser_version)
);
CREATE INDEX IF NOT EXISTS idx_document_parse_artifacts_scope
    ON document_parse_artifacts(tenant_id, parser_version, updated_at);
CREATE INDEX IF NOT EXISTS idx_document_parse_artifacts_source
    ON document_parse_artifacts(tenant_id, source_document_id);
