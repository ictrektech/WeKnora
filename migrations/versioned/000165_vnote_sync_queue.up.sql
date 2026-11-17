CREATE TABLE vnote_sync_roots (
 id varchar(36) PRIMARY KEY,
 user_id varchar(128) NOT NULL,
 workspace_id varchar(128) NOT NULL,
 subject varchar(255) NOT NULL,
 tenant_id bigint NOT NULL,
 root_id varchar(128) NOT NULL,
 config_version bigint NOT NULL,
 root_name text NOT NULL DEFAULT '',
 phase varchar(16) NOT NULL DEFAULT 'snapshot',
 scan_id varchar(36) NOT NULL,
 sync_identity varchar(255) NOT NULL DEFAULT '',
 graph_revision bigint NOT NULL DEFAULT 0,
 cursor bigint NOT NULL DEFAULT 0,
 cursor_stamp varchar(64) NOT NULL DEFAULT '',
 after_notebook_id varchar(128) NOT NULL DEFAULT '',
 after_note_id varchar(128) NOT NULL DEFAULT '',
 lease_token varchar(36) NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ NOT NULL,
 next_poll_at TIMESTAMPTZ NOT NULL,
 next_metadata_at TIMESTAMPTZ NOT NULL,
 failures integer NOT NULL DEFAULT 0,
 error_code varchar(64) NOT NULL DEFAULT '',
 UNIQUE(user_id,workspace_id,root_id)
);
CREATE INDEX vnote_sync_roots_due ON vnote_sync_roots(next_poll_at,id);
CREATE TABLE vnote_sync_notes (
 id varchar(36) PRIMARY KEY,
 sync_root_id varchar(36) NOT NULL,
 note_id varchar(128) NOT NULL,
 source_hash varchar(64) NOT NULL DEFAULT '',
 title text NOT NULL DEFAULT '',
 operation varchar(16) NOT NULL,
 revision bigint NOT NULL DEFAULT 1,
 scan_id varchar(36) NOT NULL,
 status varchar(16) NOT NULL DEFAULT 'pending',
 knowledge_id varchar(36) NOT NULL DEFAULT '',
 attempt varchar(36) NOT NULL DEFAULT '',
 next_attempt_at TIMESTAMPTZ NOT NULL,
 error_code varchar(64) NOT NULL DEFAULT '',
 UNIQUE(sync_root_id,note_id)
);
CREATE INDEX vnote_sync_notes_pending ON vnote_sync_notes(sync_root_id,status,next_attempt_at,id);
