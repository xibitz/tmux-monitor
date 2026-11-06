# ADR 0001 — PTY viewer and tmux client sizing

Date: 7 November 2026. Status: provisional, experimental read-only policy.

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

The extended spike now tests without any desktop client, with 120×39 and 90×30
windows, a smaller read-only PTY, PTY resize, window selection and detach. On tmux
3.8 macOS, setting the disposable server's global default and both windows to
`window-size manual` preserves both sizes. The CI workflow runs this same spike
on Linux, in addition to the real SSH+PTY fixture in the Go integration suite.

CI initially found that Ubuntu's tmux 3.6a exits unexpectedly on `new-window`
after the disposable server's global manual option is set. This matches
[upstream #4849](https://github.com/tmux/tmux/pull/4849), which describes a NULL
window sizing crash and its fix. The minimum was raised from 3.4 to **3.7**;
older servers are rejected by the viewer, and the spike refuses to run on them.
CI now builds upstream tmux 3.8 from its release tarball, pinned by SHA256, and
checks the selected binary version before executing integration tests.

The initial viewer is disabled by default and requires operator opt-in. It
requires tmux >=3.7 and freshly validates global/effective manual sizing before
attach, immediately afterward and periodically while viewing. The application
never sets remote options. Automatic sizing is rejected; the fixed PTY grid is
the current active window plus status rows, not the phone viewport. Changing
window/size/policy/epoch closes the viewer. External changes can affect tmux
before periodic detection; this is not an atomic lock on operator actions.
This conservative policy trades automatic desktop resizing for an explicit,
testable no-resize policy in the experimental viewer. See the operator README.

The read-only connector exposes Reader/Close/Size/Check only. It holds SSH stdin
open without writes. Interactive activation, window navigation and viewport
resizing are not implemented; do not infer their safety from read-only tests.

This evidence supports the planned PTY session view. It does not establish a
minimum supported tmux version, independent per-pane navigation, epoch collision
safety, correct interactive toggling, SSH transport behavior, or mobile browser
input handling. Linux/macOS version coverage and physical phone tests remain
open under T01/T02. Minimum 3.7 is enforced for the experimental slice; broader
version/OS/device qualification remains open. M4 still requires
a real state machine parser, escaping, flow control, and multi-client tests.
