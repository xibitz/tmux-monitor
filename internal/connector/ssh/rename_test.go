package sshconnector

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func TestRenameOverSSHWithIsolatedTmux(t *testing.T) {
	version, err := exec.Command("tmux", "-V").Output()
	if err != nil || strings.TrimSpace(string(version)) != "tmux 3.8" {
		t.Skip("qualified tmux3.8 required")
	}
	f := setup(t)
	f.host.Socket = "rename-ssh"
	dir, err := os.MkdirTemp("/tmp", "mtm-rename-ssh-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	env := append(os.Environ(), "TMUX_TMPDIR="+dir, "TMUX=")
	run := func(command string) reply {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
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
	}
	defer run("tmux -L rename-ssh kill-server")
	if r := run("tmux -L rename-ssh -f /dev/null new-session -d -s original 'sleep 120'"); r.status != 0 {
		t.Fatal(r.stderr)
	}
	snapshot, _ := tmux.SnapshotCommand(f.host.Socket)
	var mu sync.Mutex
	permitted := map[string]bool{snapshot: true, "LC_ALL=C tmux -V": true}
	mutations := 0
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), func(command string) reply {
		if command == "LC_ALL=C id -un" {
			return reply{out: "tester\n"}
		}
		mu.Lock()
		allowed := permitted[command]
		if allowed && command != snapshot && command != "LC_ALL=C tmux -V" {
			mutations++
		}
		mu.Unlock()
		if !allowed {
			return reply{status: 127}
		}
		return run(command)
	})
	manager := manager(f, address)
	fresh := func() (tmux.Inventory, []byte) {
		t.Helper()
		r := run(snapshot)
		inv, err := tmux.Parse(f.host.ID, f.host.Socket, "3.8", []byte(r.out))
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(inv.Sessions, func(i, j int) bool { return inv.Sessions[i].ID < inv.Sessions[j].ID })
		return inv, []byte(r.out)
	}
	inv, output := fresh()
	request := tmux.RenameRequest{Name: "renamed", Epoch: inv.Epoch, Revision: inv.Sessions[0].RenameRevision, Confirmed: true}
	command, err := tmux.RenameCommand(f.host.ID, f.host.Socket, "$0", request, output)
	if err != nil {
		t.Fatal(err)
	}
	permitted[command] = true
	authorize := func(context.Context) error { return nil }
	if err = manager.RenameSession(context.Background(), f.host, f.record, "$0", request, authorize); err != nil {
		t.Fatal(err)
	}
	if err = manager.RenameSession(context.Background(), f.host, f.record, "$0", request, authorize); Code(err) != "conflict" {
		t.Fatalf("stale accepted: %v", err)
	}
	run("tmux -L rename-ssh new-session -d -s duplicate 'sleep 120'")
	inv, output = fresh()
	request.Revision = inv.Sessions[0].RenameRevision
	request.Name = "duplicate"
	command, _ = tmux.RenameCommand(f.host.ID, f.host.Socket, "$0", request, output)
	mu.Lock()
	permitted[command] = true
	mu.Unlock()
	if err = manager.RenameSession(context.Background(), f.host, f.record, "$0", request, authorize); Code(err) != "duplicate_name" {
		t.Fatalf("duplicate accepted: %v", err)
	}
	inv, output = fresh()
	request.Revision = inv.Sessions[0].RenameRevision
	request.Name = "after-race"
	command, err = tmux.RenameCommand(f.host.ID, f.host.Socket, "$0", request, output)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	permitted[command] = true
	mu.Unlock()
	if err = manager.RenameSession(context.Background(), f.host, f.record, "$0", request, func(context.Context) error {
		run("tmux -L rename-ssh rename-session -t '$0' external-change")
		return nil
	}); Code(err) != "conflict" {
		t.Fatalf("TOCTOU rename accepted: %v", err)
	}
	inv, output = fresh()
	request.Revision = inv.Sessions[0].RenameRevision
	command, _ = tmux.RenameCommand(f.host.ID, f.host.Socket, "$0", request, output)
	mu.Lock()
	permitted[command] = true
	mu.Unlock()
	if err = manager.RenameSession(context.Background(), f.host, f.record, "$0", request, func(context.Context) error { return context.Canceled }); Code(err) != "access_revoked" {
		t.Fatal("dispatch after revoke")
	}
	// A restart reuses $0; the old command must reject the new server even if
	// a new session happens to have the same old name and creation second.
	run("tmux -L rename-ssh kill-server")
	run("tmux -L rename-ssh -f /dev/null new-session -d -s external-change 'sleep 120'")
	if r := run(command); r.status != 0 || r.out != "MTM_CONFLICT\n" {
		t.Fatalf("restart guard %q %s", r.out, r.stderr)
	}
	if err = manager.RenameSession(context.Background(), f.host, f.record, "$0", request, authorize); Code(err) != "conflict" {
		t.Fatal("old epoch accepted after restart")
	}
	mu.Lock()
	count := mutations
	mu.Unlock()
	if count != 3 {
		t.Fatalf("unexpected mutation dispatches %d", count)
	}
}
