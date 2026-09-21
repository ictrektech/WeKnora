ALTER TABLE archive_documents
    ADD COLUMN IF NOT EXISTS mirror_run_id VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE archive_documents
    ADD COLUMN IF NOT EXISTS mirror_lease_until TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_archive_documents_mirror_lease
    ON archive_documents(tenant_id, mirror_status, mirror_lease_until)
    WHERE trashed_at IS NULL
      AND mirror_status IN ('pending', 'processing');
