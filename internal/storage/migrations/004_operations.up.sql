CREATE TABLE tmux_operations (
 actor_id TEXT NOT NULL,
 key_hash TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 operation_id TEXT NOT NULL UNIQUE,
 host_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK (state IN ('pending','done')),
 status_code INTEGER NOT NULL DEFAULT 409,
 result_code TEXT NOT NULL DEFAULT 'operation_uncertain',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(actor_id,key_hash)
);
