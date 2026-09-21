-- Migration: 000125_message_artifacts_deleted_at
-- Users can now delete a generated file from the artifact library or the
-- in-chat artifact panel. The row is kept as a tombstone rather than removed:
--
--   * message_artifacts.position is the address the download endpoint uses
--     (msg.Artifacts[index]); physically removing a middle row would shift
--     every later file's index and hand old links the wrong blob.
--   * ArtifactCollector de-duplicates sandbox files against the rows of the
--     session. Dropping the row would let the next collect re-attach the very
--     file the user just deleted, because its sandbox mtime has not moved.
--
-- The blob itself IS reclaimed (once no other owner still binds the resource),
-- so url stays on the tombstone only as a handle for a later GC retry.
DO $$ BEGIN RAISE NOTICE '[Migration 000125] Adding message_artifacts.deleted_at'; END $$;

-- Some installations recorded 000121 as applied while the table was later
-- removed (or the migration ran against a partially restored database).
-- Recreate it here so retrying this dirty migration repairs the schema before
-- adding the tombstone column instead of failing with "relation does not
-- exist". The backfill is idempotent and preserves legacy message artifacts.
CREATE TABLE IF NOT EXISTS message_artifacts (
    id VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id VARCHAR(36) NOT NULL,
    message_id VARCHAR(36) NOT NULL,
    position INTEGER NOT NULL,
    url TEXT NOT NULL DEFAULT '',
    file_name TEXT NOT NULL DEFAULT '',
    file_type VARCHAR(32) NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    content_hash VARCHAR(64) NOT NULL DEFAULT '',
    source_path TEXT NOT NULL DEFAULT '',
    mod_time VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_message_artifacts_message_position
    ON message_artifacts (message_id, position);
CREATE INDEX IF NOT EXISTS idx_message_artifacts_session_created
    ON message_artifacts (session_id, created_at);

CREATE OR REPLACE FUNCTION pg_temp.wk_try_bigint(v TEXT) RETURNS BIGINT
LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN
    RETURN v::NUMERIC::BIGINT;
EXCEPTION WHEN others THEN
    RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION pg_temp.wk_try_timestamptz(v TEXT) RETURNS TIMESTAMPTZ
LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN
    RETURN v::TIMESTAMPTZ;
EXCEPTION WHEN others THEN
    RETURN NULL;
END $$;

INSERT INTO message_artifacts (
    session_id, message_id, position, url, file_name, file_type, file_size,
    content_hash, source_path, mod_time, created_at
)
SELECT
    m.session_id,
    m.id,
    (a.ord - 1)::INTEGER,
    COALESCE(a.elem ->> 'url', ''),
    COALESCE(a.elem ->> 'file_name', ''),
    LEFT(COALESCE(a.elem ->> 'file_type', ''), 32),
    COALESCE(pg_temp.wk_try_bigint(a.elem ->> 'file_size'), 0),
    LEFT(COALESCE(a.elem ->> 'content_hash', ''), 64),
    COALESCE(a.elem ->> 'source_path', ''),
    LEFT(COALESCE(a.elem ->> 'mod_time', ''), 64),
    COALESCE(pg_temp.wk_try_timestamptz(a.elem ->> 'created_at'), m.created_at, CURRENT_TIMESTAMP)
FROM messages m
CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(m.artifacts) = 'array' THEN m.artifacts ELSE '[]'::jsonb END
) WITH ORDINALITY AS a(elem, ord)
WHERE jsonb_typeof(a.elem) = 'object'
ON CONFLICT (message_id, position) DO NOTHING;

UPDATE messages SET artifacts = NULL
WHERE artifacts IS NOT NULL AND artifacts <> '[]'::jsonb;

ALTER TABLE message_artifacts
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP WITH TIME ZONE;

-- The artifact library and both list endpoints read live rows only; the
-- partial index keeps those scans off the tombstones.
CREATE INDEX IF NOT EXISTS idx_message_artifacts_session_live
    ON message_artifacts (session_id, created_at)
    WHERE deleted_at IS NULL;

-- Before a delete reclaims an object's bytes it checks whether any live row
-- anywhere still points at the same url (a forked session's copied rows, a
-- later answer that re-attached the file). That lookup is by url alone.
CREATE INDEX IF NOT EXISTS idx_message_artifacts_url_live
    ON message_artifacts (url)
    WHERE deleted_at IS NULL;

COMMENT ON COLUMN message_artifacts.deleted_at IS 'Set when the user deleted the file; the row survives to keep position stable and to stop the collector re-attaching it';
