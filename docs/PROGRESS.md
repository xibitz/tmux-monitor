# Implementation progress

Updated: 6 November 2026.

| Task | State | Evidence / remaining work |
| --- | --- | --- |
| T00 repository, builds, migrations, health | Implemented | Go/React structure, lockfiles, Makefile, CI workflow, embedded web build, `/healthz`, transactional migration and rollback tests. Hosted CI run is a separate result. |
| T01 repeatable device/host matrix | Open | Only this macOS machine was exercised; Linux SSH host, Agent host, Android/iOS are not yet configured. |
| T02 tmux multi-client spike | Partial | Isolated tmux 3.8 probe passes for normal desktop plus read-only/ignore-size mobile. [ADR](adr/0001-tmux-client-spike.md) records constraints; version matrix/minimum and no-desktop policy remain open. |
| T03 access foundation | Partial | Localhost-only listener, local bootstrap, Argon2id, session expiry/revoke, Origin/CSRF, default-deny host permissions, audit skeleton and negative tests implemented. Live Tailscale Serve/Grants negative access tests remain open. |
| T10 SSH registry / onboarding | Partial | Profile CRUD and administrator address allowlist implemented. Secret references, host key verification, health/capability probe and SSH connections remain open. |
| T11–T60 | Open | No terminal transport, tmux mutation endpoint, Agent, independent pane view, notification, or production packaging yet. |

The UI shows actual profile data, unavailable connection status, login errors,
empty/filter states and audit records. There is no simulated live inventory.
No terminal-related P0 release gate is claimed complete by this milestone.

## Local verification

- `make check`, `make test` (`go test -race ./...`), and `make build` pass.
- A Linux amd64 binary builds with `CGO_ENABLED=0`.
- Browser checks with a disposable database pass: login, SSH profile creation,
  persistence after page reload, details dialog, activity log, and logout.
  At a 393×852 viewport, document width equals viewport width; the browser
  reports no warning or error messages during the checked flow. This is browser
  viewport testing, not a physical Android/iOS result.
- The isolated local tmux probe passes; limitations are recorded in the ADR.
- The hosted workflow has been added; a GitHub Actions result is not included
  in this local evidence.

Next implementation slice: T10 secret references + explicit host-key pinning,
SSH probe, T11 inventory/epoch, then T12 PTY read-only and bounded WebSocket
gateway after the remaining tmux sizing behavior is resolved.
