CREATE TABLE coding_notices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('codex','claude')),
    kind TEXT NOT NULL CHECK (kind IN ('turn_completed','attention_required','error','unknown')),
    confidence TEXT NOT NULL CHECK (confidence IN ('verified_pane','unknown')),
    epoch TEXT NOT NULL,
    session_id TEXT NOT NULL,
    pane_id TEXT NOT NULL,
    dedupe TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE (host_id, dedupe)
);
CREATE INDEX coding_notices_host_recent ON coding_notices(host_id,id DESC);

CREATE TABLE push_subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    encrypted_endpoint BLOB NOT NULL,
    last_sent_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX push_subscriptions_user ON push_subscriptions(user_id);
