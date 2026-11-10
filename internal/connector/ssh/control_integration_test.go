package sshconnector

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestWatchInventoryOverSSHWithIsolatedTmux(t *testing.T) {
	version, err := exec.Command("tmux", "-V").Output()
	if err != nil || strings.TrimSpace(string(version)) != "tmux 3.8" {
		t.Skip("qualified tmux 3.8 required")
	}
	f := setup(t)
	f.host.Socket = "qualify-events"
	dir, err := os.MkdirTemp("/tmp", "mtm-events-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	env := append(os.Environ(), "TMUX_TMPDIR="+dir, "TMUX=", "TERM=xterm-256color")
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "tmux", append([]string{"-L", f.host.Socket, "-f", "/dev/null"}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture tmux %s: %v %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("new-session", "-d", "-s", "events", "-x", "100", "-y", "30", "sleep 120")
	defer run("kill-server")
	dimensions := run("display-message", "-p", "-t", "events", "#{window_width}x#{window_height}")
	m := manager(f, terminalServer(t, f, env))
	ctx, stop := context.WithTimeout(context.Background(), 7*time.Second)
	defer stop()
	changed := make(chan struct{}, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- m.WatchInventory(ctx, f.host, f.record, func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		})
	}()
	for !strings.Contains(run("list-clients", "-F", "#{client_flags}"), "no-output") {
		select {
		case err := <-finished:
			t.Fatalf("watch ended before attach: %v", err)
		case <-ctx.Done():
			t.Fatal("watch did not attach")
		case <-time.After(40 * time.Millisecond):
		}
	}
	select {
	case <-changed:
	default:
	}
	run("rename-window", "-t", "events", "external-change")
	select {
	case <-changed:
	case err := <-finished:
		t.Fatalf("watch ended before invalidation: %v", err)
	case <-ctx.Done():
		t.Fatal("external rename did not trigger invalidation")
	}
	stop()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop")
	}
	if got := run("display-message", "-p", "-t", "events", "#{window_width}x#{window_height}"); got != dimensions {
		t.Fatalf("watch changed tmux dimensions: %s → %s", dimensions, got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for run("list-clients", "-F", "#{client_name}") != "" {
		if time.Now().After(deadline) {
			t.Fatal("watch client did not detach")
		}
		time.Sleep(30 * time.Millisecond)
	}
	desktopCtx, stopDesktop := context.WithCancel(context.Background())
	desktop := exec.CommandContext(desktopCtx, "tmux", "-L", f.host.Socket, "attach-session", "-t", "events")
	desktop.Env = env
	desktopPTY, err := pty.StartWithSize(desktop, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		stopDesktop()
		t.Fatal(err)
	}
	defer func() {
		stopDesktop()
		_ = desktopPTY.Close()
		_ = desktop.Wait()
	}()
	deadline = time.Now().Add(2 * time.Second)
	for run("list-clients", "-F", "#{client_name}") == "" {
		if time.Now().After(deadline) {
			t.Fatal("desktop client did not attach")
		}
		time.Sleep(30 * time.Millisecond)
	}
	run("set-option", "-t", "events", "destroy-unattached", "on")
	defer run("set-option", "-t", "events", "destroy-unattached", "off")
	unsafeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.WatchInventory(unsafeCtx, f.host, f.record, func() {}); err != nil || unsafeCtx.Err() != nil {
		t.Fatalf("expected immediate refusal of unsafe attach: %v", err)
	}
	if clients := run("list-clients", "-F", "#{client_flags}"); strings.Contains(clients, "no-output") || strings.Contains(clients, "\n") {
		t.Fatal("control watcher attached despite destructive policy")
	}
	run("has-session", "-t", "events")
}
