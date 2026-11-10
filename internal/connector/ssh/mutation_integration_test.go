package sshconnector

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func TestLifecycleAndLayoutOverSSHWithIsolatedTmux(t *testing.T) {
	version, err := exec.Command("tmux", "-V").Output()
	if err != nil || strings.TrimSpace(string(version)) != "tmux 3.8" {
		t.Skip("qualified tmux 3.8 required")
	}
	f := setup(t)
	f.host.Socket = "qualify-layout"
	dir, err := os.MkdirTemp("/tmp", "mtm-layout-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	env := append(os.Environ(), "TMUX_TMPDIR="+dir, "TMUX=", "TERM=xterm-256color")
	run := func(command string) reply {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Env = env
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return reply{status: 1, stderr: stderr.String()}
		}
		return reply{out: string(out)}
	}
	defer run("tmux -L qualify-layout kill-server")
	if r := run("tmux -L qualify-layout -f /dev/null new-session -d -s original 'printf snapshot-marker; sleep 120'"); r.status != 0 {
		t.Fatal(r.stderr)
	}
	ctx := context.Background()
	snapshot, _ := tmux.SnapshotCommand(f.host.Socket)
	var permitted sync.Map
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), func(command string) reply {
		switch command {
		case "LC_ALL=C id -un":
			return reply{out: "tester\n"}
		case snapshot, "LC_ALL=C tmux -V":
			return run(command)
		}
		if _, ok := permitted.Load(command); !ok {
			return reply{status: 127}
		}
		return run(command)
	})
	m := manager(f, address)
	fresh := func() (tmux.Inventory, []byte) {
		t.Helper()
		data := []byte(run(snapshot).out)
		inv, err := tmux.Parse(f.host.ID, f.host.Socket, "3.8", data)
		if err != nil {
			t.Fatal(err)
		}
		return inv, data
	}
	permit := func(command string, err error) {
		t.Helper()
		if err != nil || command == "" {
			t.Fatalf("catalog: %v", err)
		}
		permitted.Store(command, true)
	}
	authorize := func(context.Context) error { return nil }

	inv, data := fresh()
	original := inv.Sessions[0]
	first := original.Windows[0].Panes[0]
	capture := tmux.PaneSnapshotRequest{Epoch: inv.Epoch, Revision: first.Revision}
	permit(tmux.PaneSnapshotCommand(f.host.ID, f.host.Socket, first.ID, capture, data))
	captured, err := m.CapturePane(ctx, f.host, f.record, first.ID, capture, authorize)
	if err != nil || !strings.Contains(captured.Text, "snapshot-marker") {
		t.Fatalf("pane snapshot missing marker: %+v %v", captured, err)
	}
	create := tmux.CreateSessionRequest{Name: "created", Epoch: inv.Epoch, Revision: inv.CreateRevision, Confirmed: true}
	permit(tmux.CreateSessionCommand(f.host.ID, f.host.Socket, create, data))
	created, err := m.CreateSession(ctx, f.host, f.record, create, authorize)
	if err != nil || !tmux.ValidSessionID(created) || created == original.ID {
		t.Fatalf("create session: %q %v", created, err)
	}
	if _, err := m.CreateSession(ctx, f.host, f.record, create, authorize); Code(err) != "conflict" {
		t.Fatalf("stale create: %v", err)
	}
	target := func(id string) (tmux.Inventory, tmux.Session, []byte) {
		t.Helper()
		latest, raw := fresh()
		for _, session := range latest.Sessions {
			if session.ID == id {
				return latest, session, raw
			}
		}
		t.Fatalf("session missing: %s", id)
		return latest, tmux.Session{}, raw
	}
	inv, session, data := target(created)
	createWindow := tmux.CreateWindowRequest{Name: "second", Epoch: inv.Epoch, Revision: session.Revision, Confirmed: true}
	permit(tmux.CreateWindowCommand(f.host.ID, f.host.Socket, created, createWindow, data))
	windowID, err := m.CreateWindow(ctx, f.host, f.record, created, createWindow, authorize)
	if err != nil || !tmux.ValidWindowID(windowID) {
		t.Fatalf("create window: %q %v", windowID, err)
	}
	window := func() (tmux.Window, []byte) {
		t.Helper()
		_, session, raw := target(created)
		for _, w := range session.Windows {
			if w.ID == windowID {
				if w.Active {
					t.Fatal("detached window became active")
				}
				return w, raw
			}
		}
		t.Fatal("created window missing")
		return tmux.Window{}, nil
	}
	w, data := window()
	rename := tmux.RenameWindowRequest{SessionID: created, Name: "renamed", Epoch: inv.Epoch, Revision: w.Revision, Confirmed: true}
	permit(tmux.RenameWindowCommand(f.host.ID, f.host.Socket, windowID, rename, data))
	if _, err = m.RenameWindow(ctx, f.host, f.record, windowID, rename, authorize); err != nil {
		t.Fatal(err)
	}
	w, data = window()
	if w.Name != "renamed" {
		t.Fatalf("window was not renamed: %s", w.Name)
	}
	layout := func(w tmux.Window) tmux.LayoutWindowRequest {
		return tmux.LayoutWindowRequest{SessionID: created, Epoch: inv.Epoch, Revision: w.Revision, Confirmed: true}
	}
	split := tmux.SplitPaneRequest{PaneLayoutRequest: tmux.PaneLayoutRequest{LayoutWindowRequest: layout(w), WindowID: windowID}, Direction: "side-by-side"}
	permit(tmux.SplitPaneCommand(f.host.ID, f.host.Socket, w.Panes[0].ID, split, data))
	paneID, err := m.SplitPane(ctx, f.host, f.record, w.Panes[0].ID, split, authorize)
	if err != nil || !tmux.ValidPaneID(paneID) {
		t.Fatalf("split: %q %v", paneID, err)
	}
	w, data = window()
	if len(w.Panes) != 2 {
		t.Fatalf("expected split panes: %+v", w.Panes)
	}
	killPane := tmux.KillPaneRequest{PaneLayoutRequest: tmux.PaneLayoutRequest{LayoutWindowRequest: layout(w), WindowID: windowID}, Confirmation: paneID}
	permit(tmux.KillPaneCommand(f.host.ID, f.host.Socket, paneID, killPane, data))
	if _, err = m.KillPane(ctx, f.host, f.record, paneID, killPane, authorize); err != nil {
		t.Fatal(err)
	}
	w, data = window()
	if len(w.Panes) != 1 || w.Panes[0].ID == paneID {
		t.Fatalf("pane was not removed: %+v", w.Panes)
	}
	killWindow := tmux.KillWindowRequest{LayoutWindowRequest: layout(w), Confirmation: windowID}
	permit(tmux.KillWindowCommand(f.host.ID, f.host.Socket, windowID, killWindow, data))
	if _, err = m.KillWindow(ctx, f.host, f.record, windowID, killWindow, authorize); err != nil {
		t.Fatal(err)
	}
	inv, session, data = target(created)
	if len(session.Windows) != 1 {
		t.Fatalf("window was not removed: %+v", session.Windows)
	}
	kill := tmux.KillSessionRequest{Epoch: inv.Epoch, Revision: session.Revision, Confirmation: created, Confirmed: true}
	permit(tmux.KillSessionCommand(f.host.ID, f.host.Socket, created, kill, data))
	if _, err = m.KillSession(ctx, f.host, f.record, created, kill, authorize); err != nil {
		t.Fatal(err)
	}
	inv, _ = fresh()
	if len(inv.Sessions) != 1 || inv.Sessions[0].ID != original.ID {
		t.Fatalf("expected only original session: %+v", inv.Sessions)
	}
}
