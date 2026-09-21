ALTER TABLE archive_documents
    ADD COLUMN IF NOT EXISTS extraction_progress INTEGER NOT NULL DEFAULT 0;
