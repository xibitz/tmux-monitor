# ADR 0004: session create/kill source — untested

Status: development only, 7 November2026. User explicitly requested continuing
development without testing. No build, compiler, test, browser or CI result is
claimed. ADR0003 remains the previously verified rename baseline.

Create/kill require both the existing mutations flag and a separate default-off
TMUX_MONITOR_SESSION_LIFECYCLE_ENABLED flag. This prevents enabling the unqualified
operations merely by retaining an existing rename setting. Rename dispatch stays
in its previous handler; new create/kill share a separate executor and ledger.

Create uses a createRevision over server epoch plus sorted session identities,
creation times and names. It excludes pane activity. It requires an existing
server: tmux global -N prevents implicit bootstrap. The final S: loop guard checks
the session set and each identity/name before detached new-session. Geometry120x40
belongs only to the new session; no custom command/environment/cwd is accepted.

Kill requires tmux:kill plus tmux:read, exact typed native ID and explicit intent.
It uses the full observed session revision and final guards for the target plus
W:/P: membership and observed primitive fields. It does not issue kill-server.
Linked windows follow tmux ownership rules; killing a session does not promise
that every linked process terminates. Input revocation cannot undo an operation
already in flight. Hash comparisons cannot detect changes away and back to the
same state, and unobserved layout properties are not a global desktop lock.

The loop construction was reviewed against the installed3.8 man page and upstream
[argument handling](https://github.com/tmux/tmux/blob/3.8/arguments.c) and
[format contexts](https://github.com/tmux/tmux/blob/3.8/format.c).
This is source reasoning, not executable evidence. Guard conjunctions use balanced
trees to bound recursion depth. Limits are64 items per collection and64KiB after
shell quoting, with early construction limits. Special format literals, nested
loop expansion and last-session output are specifically pending qualification.

Schema005 adds result_target to existing durable reservations. Create stores its
returned native ID so a replay does not resolve a different session by name. The
request hash includes action/host/target/credential/payload, so a key shared across
rename/create/kill conflicts. Intent and cache invalidation commit before dispatch;
completion metadata and audit commit together; pending/ambiguous never retry.
The existing10000-record/no-prune policy is retained. No names or raw keys are
stored in the operation metadata/audit. Downgrade loses result IDs; no downgrade
or record deletion is a supported recovery/retry mechanism.

Pending acceptance is listed in IMPLEMENTATION_PLAN.md and MEMORY.md. Do not mark
T20 DONE or cite the earlier rename CI as evidence for this addition. User's latest
no-testing instruction takes precedence over local workflow defaults; commits for
this checkpoint use [skip ci] without altering the repository's CI workflow.
