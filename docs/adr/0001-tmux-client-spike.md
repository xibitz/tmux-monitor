# ADR 0001 — PTY viewer and tmux client sizing

Date: 6 November 2026. Status: provisional, local evidence only.

The first terminal implementation will attach a separate PTY client to a session.
Read-only must be enforced by both gateway input rejection and the tmux client
flag. Mobile viewers should use `ignore-size`; ordinary desktop clients keep
their sizing role. Closing a viewer only closes that client.

## Experiment

`python3 scripts/tmux-spike.py` creates an isolated tmux server using a temporary
socket, a desktop PTY at 120×40 and a mobile PTY at 40×20. It cleans up that
disposable server afterward. It does not use the user's default tmux socket.

Observed on macOS arm64 with tmux 3.8:

- The normal desktop client establishes a 120×39 window (one status row).
- Attaching a `read-only,ignore-size` mobile client preserves 120×39.
- Sending prefix + `c` through the read-only client does not create a window.
- Terminating the mobile client leaves the session alive and size unchanged.
- `pid`, `start_time`, and `socket_path` are available for epoch research.
- A basic control client emits `%begin` and `%end` blocks.

An initial variant set `ignore-size` on **both** clients. With no ordinary sizing
client left, the window changed from 120×39 to 40×19. The implementation must
therefore test the no-sizing-client case explicitly and must not assume that
setting a flag alone establishes an invariant for every tmux configuration.

## Consequences and remaining gates

This evidence supports the planned PTY session view. It does not establish a
minimum supported tmux version, independent per-pane navigation, epoch collision
safety, correct interactive toggling, SSH transport behavior, or mobile browser
input handling. Linux/macOS version coverage and physical phone tests remain
open under T01/T02. M1 must test connection without a desktop client and choose
a safe window-size policy before terminal access is enabled. M4 still requires
a real state machine parser, escaping, flow control, and multi-client tests.
