DROP INDEX IF EXISTS idx_archive_documents_mirror_lease;
ALTER TABLE archive_documents DROP COLUMN IF EXISTS mirror_lease_until;
ALTER TABLE archive_documents DROP COLUMN IF EXISTS mirror_run_id;
