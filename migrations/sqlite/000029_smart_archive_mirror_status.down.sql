DROP INDEX IF EXISTS idx_archive_documents_mirror_pending;
ALTER TABLE archive_documents DROP COLUMN mirror_error_message;
ALTER TABLE archive_documents DROP COLUMN mirror_status;

