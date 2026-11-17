CREATE TABLE vnote_controls (
  user_id VARCHAR(128) NOT NULL,
  workspace_id VARCHAR(128) NOT NULL,
  subject VARCHAR(255) NOT NULL,
  version BIGINT NOT NULL,
  enabled BOOLEAN NOT NULL,
  roots_json TEXT NOT NULL,
  PRIMARY KEY (user_id, workspace_id)
);
