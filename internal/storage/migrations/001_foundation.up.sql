CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0,1)),
    disabled_at TEXT,
    created_at TEXT NOT NULL
);
CREATE TABLE app_sessions (
    id_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX sessions_expiry ON app_sessions(expires_at);
CREATE TABLE hosts (
    id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    transport TEXT NOT NULL CHECK (transport IN ('ssh','agent')),
    tailnet_address TEXT NOT NULL DEFAULT '',
    os_user TEXT NOT NULL,
    socket_name TEXT NOT NULL,
    environment TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE host_access (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action IN ('host:read','tmux:read','tmux:write','tmux:kill','terminal:write','host:admin')),
    PRIMARY KEY (user_id, host_id, action)
);
CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    host_id TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL,
    request_id TEXT NOT NULL,
    at TEXT NOT NULL
);
CREATE INDEX audit_time ON audit_events(at);
