CREATE TABLE vnote_delegations (
    user_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    tenant_id INTEGER NOT NULL,
    subject TEXT NOT NULL,
    secret_hash TEXT NOT NULL UNIQUE,
    secret_encrypted TEXT NOT NULL,
    PRIMARY KEY (user_id, workspace_id)
);
