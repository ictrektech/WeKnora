-- Migration: 000079_upstream_wiki_and_tenant_api_key_repair
-- Catch up installations that already used ictrek versions 000075-000078
-- before upstream assigned the same migration numbers.

ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS last_edit_source VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE wiki_pages ADD COLUMN IF NOT EXISTS last_editor_id VARCHAR(64) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS wiki_page_revisions (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    page_id VARCHAR(36) NOT NULL,
    slug VARCHAR(255) NOT NULL,
    version INT NOT NULL,
    title VARCHAR(512) NOT NULL DEFAULT '',
    page_type VARCHAR(32) NOT NULL DEFAULT 'summary',
    status VARCHAR(32) NOT NULL DEFAULT 'published',
    content TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    aliases JSONB DEFAULT '[]'::JSONB,
    edit_source VARCHAR(16) NOT NULL DEFAULT '',
    editor_id VARCHAR(64) NOT NULL DEFAULT '',
    edited_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_wiki_page_revisions_page_version
    ON wiki_page_revisions (page_id, version);
CREATE INDEX IF NOT EXISTS idx_wiki_page_revisions_kb_slug
    ON wiki_page_revisions (knowledge_base_id, slug);

DO $$ BEGIN RAISE NOTICE '[Migration 000079] Repairing tenant API key scope columns...'; END $$;

ALTER TABLE tenant_api_keys
    ADD COLUMN IF NOT EXISTS scope_type VARCHAR(16) NOT NULL DEFAULT 'tenant';

ALTER TABLE tenant_api_keys
    ALTER COLUMN tenant_id DROP NOT NULL;

ALTER TABLE tenant_api_keys
    DROP CONSTRAINT IF EXISTS chk_tenant_api_keys_scope;

ALTER TABLE tenant_api_keys
    ADD CONSTRAINT chk_tenant_api_keys_scope CHECK (
        (scope_type = 'tenant' AND tenant_id IS NOT NULL)
        OR (scope_type = 'platform' AND tenant_id IS NULL AND full_access = FALSE)
    );

CREATE INDEX IF NOT EXISTS idx_tenant_api_keys_scope_type
    ON tenant_api_keys(scope_type);

DO $$ BEGIN RAISE NOTICE '[Migration 000079] Upstream Wiki and tenant API key schema ready'; END $$;
