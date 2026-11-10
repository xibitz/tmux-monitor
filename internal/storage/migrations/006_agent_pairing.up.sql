CREATE TABLE agent_identities (
    id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    credential_hash TEXT NOT NULL UNIQUE,
    tailnet_ip TEXT NOT NULL,
    generation INTEGER NOT NULL DEFAULT 0 CHECK (generation >= 0),
    created_at TEXT NOT NULL,
    revoked_at TEXT
);
CREATE UNIQUE INDEX agent_active_host ON agent_identities(host_id) WHERE revoked_at IS NULL;

CREATE TABLE enrollment_tokens (
    host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    tailnet_ip TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
