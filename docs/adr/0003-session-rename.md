# ADR 0003: guarded rename and durable operation reservations

Status: implemented experimental slice, 7 November 2026. T20 remains PARTIAL.

Session rename is the first administrative mutation. It requires explicit
operator opt-in and tmux3.8, the version qualified by integration tests. Create,
kill, window/pane mutations and Agent parity remain separate work.

The observation tree revision changes with pane/process activity. Rename uses a
separate renameRevision over epoch/session ID/creation/name, so unrelated process
activity does not prevent renaming. The connector checks a fresh snapshot and
constructs a final server-side guard over pid/start/socket/version plus the target
identity/name. No cached snapshot authorizes mutation.

The tmux3.8 [if-shell implementation](https://github.com/tmux/tmux/blob/3.8/cmd-if-shell.c)
evaluates `-F` without starting a shell job and inserts its selected branch after
the current item. The [command queue](https://github.com/tmux/tmux/blob/3.8/cmd-queue.c)
continues until empty or a waiting command. This catalog places rename immediately
after that guard, with no waiting command in between. It never targets a session
by name. Native IDs are not enough by themselves because restart can reuse IDs.
Format literals and shell argv are escaped separately; host-controlled names and
paths are never expanded with E:/T:. Integration tests cover literal format syntax,
special names/paths, external rename before dispatch, duplicate names and restart.

Hash revisions detect state differences, not an external change away and back to
the same state. The guarantee is identity and name preconditions for rename, not
an atomic lock across a preceding SSH observation or all desktop activity. Hooks
remain the host operator's code; a failure/lost acknowledgement after dispatch can
leave an uncertain result. No retry or compensating rename is attempted.

SQLite cannot transact atomically with remote tmux. Migration004 therefore stores
a durable pending reservation and audit intent before sending anything. Completion
and result audit commit together. Crash, lost reply or completion-storage failure
leaves a reservation that forbids dispatch on the same key after restart. Replays
recheck access, bind the request/credential hash, and return the original metadata
outcome. Names, raw keys and command contents are not stored in this ledger/audit.

Records do not expire automatically: expiry would permit an old uncertain request
to execute again. The installation limit is10000 records, after which new operations
are refused. Retention/archival and migration of these tombstones need an explicit
future policy. Downgrading to schema3 deletes the ledger and its replay protection;
the CLI requires explicit data-loss acknowledgement for downgrade. Back up first.

Fresh login/ACL/credential revalidation occurs immediately before dispatch. A
revocation after dispatch cannot undo a remote operation already in flight. UI
confirmation describes the shared effect, and any failure requires inventory
refresh before a new intent. Closing the dialog does not retry/cancel/undo an
operation that may already have reached tmux.
