ALTER TABLE knowledge_bases ADD COLUMN private_owner_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE knowledge_bases ADD COLUMN v_note_root_id VARCHAR(255) NOT NULL DEFAULT '';
CREATE INDEX idx_private_kb_owner ON knowledge_bases(private_owner_id) WHERE private_owner_id <> '';
CREATE TABLE vnote_import_states (
  id VARCHAR(36) PRIMARY KEY, user_id VARCHAR(128) NOT NULL, knowledge_base_id VARCHAR(36) NOT NULL,
  knowledge_id VARCHAR(36) NOT NULL DEFAULT '', content_hash VARCHAR(64) NOT NULL DEFAULT ''
);
CREATE TABLE vnote_import_gates (
  id VARCHAR(32) PRIMARY KEY, token VARCHAR(36) NOT NULL DEFAULT '', lease_until TIMESTAMPTZ NOT NULL,
  knowledge_id VARCHAR(36) NOT NULL DEFAULT '', tenant_id BIGINT NOT NULL DEFAULT 0,
  content_hash VARCHAR(64) NOT NULL DEFAULT '', attempt VARCHAR(36) NOT NULL DEFAULT ''
);
