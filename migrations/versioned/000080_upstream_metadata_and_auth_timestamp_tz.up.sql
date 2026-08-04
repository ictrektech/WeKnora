DO $$ BEGIN RAISE NOTICE '[Migration 000080] Applying metadata index and authorization timestamp repair...'; END $$;

CREATE INDEX IF NOT EXISTS idx_knowledges_kb_metadata_external_id
    ON knowledges (knowledge_base_id, (metadata->>'external_id') text_pattern_ops)
    WHERE deleted_at IS NULL;

-- Naive TIMESTAMP values were read through a GORM session with TimeZone=UTC.
-- Treat existing literals as UTC when promoting to timestamptz.
DO $$
DECLARE
    item RECORD;
BEGIN
    FOR item IN
        SELECT * FROM (VALUES
            ('tenant_api_keys', 'last_used_at'),
            ('tenant_api_keys', 'expires_at'),
            ('tenant_api_keys', 'revoked_at'),
            ('tenant_api_keys', 'created_at'),
            ('tenant_api_keys', 'updated_at'),
            ('resource_access_grants', 'expires_at'),
            ('resource_access_grants', 'revoked_at'),
            ('resource_access_grants', 'created_at')
        ) AS columns(table_name, column_name)
    LOOP
        IF EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = item.table_name
              AND column_name = item.column_name
              AND data_type = 'timestamp without time zone'
        ) THEN
            EXECUTE format(
                'ALTER TABLE %I ALTER COLUMN %I TYPE TIMESTAMP WITH TIME ZONE USING %I AT TIME ZONE ''UTC''',
                item.table_name, item.column_name, item.column_name
            );
        END IF;
    END LOOP;
END $$;

DO $$ BEGIN RAISE NOTICE '[Migration 000080] Metadata index and authorization timestamps ready'; END $$;
