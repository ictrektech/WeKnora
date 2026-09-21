-- Smart Archive mirror delivery is a derived, independently retryable step.
ALTER TABLE archive_documents ADD COLUMN mirror_status TEXT NOT NULL DEFAULT 'not_started';
ALTER TABLE archive_documents ADD COLUMN mirror_error_message TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_archive_documents_mirror_pending
    ON archive_documents(tenant_id, mirror_status, updated_at)
    WHERE trashed_at IS NULL
      AND mirror_status IN ('pending', 'processing');

