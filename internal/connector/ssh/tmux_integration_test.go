package sshconnector

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

// This SSH fixture executes only the exact command catalog against its own
// disposable TMUX_TMPDIR/socket. It never uses the user's default tmux server.
func TestSSHWithRealIsolatedTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable; CI installs it for this integration test")
	}
	f := setup(t)
	f.host.Socket = "ssh-integration"
	dir, err := os.MkdirTemp("/tmp", "mtm-ssh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	env := append(os.Environ(), "TMUX_TMPDIR="+dir, "TMUX=", "TERM=xterm-256color")
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "tmux", append([]string{"-L", f.host.Socket, "-f", "/dev/null"}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("test tmux: %v %s", err, out)
		}
		return out
	}
	t.Cleanup(func() { cmd := exec.Command("tmux", "-L", f.host.Socket, "kill-server"); cmd.Env = env; _ = cmd.Run() })
	snapshot, _ := tmux.SnapshotCommand(f.host.Socket)
	emptyCommand, _ := tmux.EmptySnapshotCommand(f.host.Socket)
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), func(command string) reply {
		if command == "LC_ALL=C id -un" {
			return reply{out: "tester\n"}
		}
		if command != "LC_ALL=C tmux -V" && command != snapshot && command != emptyCommand {
			return reply{status: 127}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Env = env
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		status := uint32(0)
		if err != nil {
			status = 1
		}
		return reply{out: string(out), stderr: stderr.String(), status: status}
	})
	m := manager(f, address)
	empty, err := m.Snapshot(context.Background(), f.host, f.record)
	if err != nil || empty.Capabilities.ServerRunning || len(empty.Sessions) != 0 {
		t.Fatalf("initial empty: %+v %v", empty, err)
	}
	run("new-session", "-d", "-s", "integration", "-x", "120", "-y", "40", "sleep 120")
	first, err := m.Snapshot(context.Background(), f.host, f.record)
	if err != nil || len(first.Sessions) != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	run("rename-session", "-t", "$0", "renamed")
	run("new-window", "-d", "-t", "$0", "-n", "extra", "sleep 120")
	run("split-window", "-d", "-t", "%0", "sleep 120")
	second, err := m.Snapshot(context.Background(), f.host, f.record)
	if err != nil || second.Sessions[0].Name != "renamed" || len(second.Sessions[0].Windows) != 2 || len(second.Sessions[0].Windows[0].Panes) != 2 {
		t.Fatalf("external changes: %+v %v", second, err)
	}
	if first.Epoch != second.Epoch || first.Sessions[0].Revision == second.Sessions[0].Revision {
		t.Fatal("incorrect epoch/revision after structural change")
	}
	run("kill-window", "-t", "@1")
	third, err := m.Snapshot(context.Background(), f.host, f.record)
	if err != nil || len(third.Sessions[0].Windows) != 1 {
		t.Fatalf("external window close: %v", err)
	}
	run("has-session", "-t", "$0") // SSH channels have closed; tmux remains alive.
	run("set-option", "-s", "exit-empty", "off")
	run("kill-session", "-t", "$0")
	withoutSessions, err := m.Snapshot(context.Background(), f.host, f.record)
	if err != nil || !withoutSessions.Capabilities.ServerRunning || len(withoutSessions.Sessions) != 0 || withoutSessions.Epoch != first.Epoch {
		t.Fatalf("running server without sessions: %+v %v", withoutSessions, err)
	}
	run("kill-server")
	run("new-session", "-d", "-s", "replacement", "sleep 120")
	restarted, err := m.Snapshot(context.Background(), f.host, f.record)
	if err != nil || restarted.Epoch == first.Epoch {
		t.Fatalf("server restart reused epoch: %v", err)
	}
}
