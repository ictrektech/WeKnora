CREATE TABLE vnote_import_generations (
  attempt VARCHAR(36) PRIMARY KEY,
  state_id VARCHAR(36) NOT NULL,
  knowledge_id VARCHAR(36) NOT NULL UNIQUE,
  expected_knowledge_id VARCHAR(36) NOT NULL DEFAULT '',
  user_id VARCHAR(128) NOT NULL,
  workspace_id VARCHAR(128) NOT NULL,
  subject VARCHAR(255) NOT NULL,
  root_id VARCHAR(128) NOT NULL,
  config_version BIGINT NOT NULL,
  content_hash VARCHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'staged'
);
CREATE INDEX idx_vnote_generation_state ON vnote_import_generations(state_id, status);
