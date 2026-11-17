CREATE TABLE vnote_delegations (
    user_id varchar(128) NOT NULL,
    workspace_id varchar(128) NOT NULL,
    tenant_id bigint NOT NULL,
    subject varchar(255) NOT NULL,
    secret_hash varchar(64) NOT NULL UNIQUE,
    secret_encrypted text NOT NULL,
    PRIMARY KEY (user_id, workspace_id)
);
