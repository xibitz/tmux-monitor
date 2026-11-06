CREATE TABLE ssh_credentials (
    host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    key_ref TEXT NOT NULL UNIQUE,
    host_public_key TEXT NOT NULL,
    host_key_fingerprint TEXT NOT NULL,
    configured_at TEXT NOT NULL
);
CREATE TABLE host_observations (
    host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('ONLINE','DEGRADED','UNREACHABLE')),
    reason TEXT NOT NULL,
    observed_at TEXT NOT NULL
);
CREATE TABLE inventory_cache (
    host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL,
    observed_at TEXT NOT NULL
);
