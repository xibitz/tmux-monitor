package tmux

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRenameCatalogWithIsolatedTmux(t *testing.T) {
	if out, err := exec.Command("tmux", "-V").Output(); err != nil || strings.TrimSpace(string(out)) != "tmux 3.8" {
		t.Skip("rename catalog qualified on tmux3.8")
	}
	dir, err := os.MkdirTemp("/tmp", "mtm#},rename-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	env := append(os.Environ(), "TMUX_TMPDIR="+dir, "TMUX=")
	run := func(command string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		c.Env = env
		return c.CombinedOutput()
	}
	defer run("tmux -L rename-catalog kill-server")
	if out, err := run("tmux -L rename-catalog -f /dev/null new-session -d -s original 'sleep 120'"); err != nil {
		t.Fatalf("setup: %s %v", out, err)
	}
	snapshot, _ := SnapshotCommand("rename-catalog")
	take := func() (Inventory, []byte) {
		t.Helper()
		out, err := run(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		inv, err := Parse("test", "rename-catalog", "3.8", out)
		if err != nil {
			t.Fatal(err)
		}
		return inv, out
	}
	inv, out := take()
	req := RenameRequest{Name: "renamed", Epoch: inv.Epoch, Revision: inv.Sessions[0].RenameRevision, Confirmed: true}
	command, err := RenameCommand("test", "rename-catalog", "$0", req, out)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := run(command); err != nil || string(result) != "MTM_RENAMED\n" {
		t.Fatalf("rename: %q %v", result, err)
	}
	if result, err := run(command); err != nil || string(result) != "MTM_CONFLICT\n" {
		t.Fatalf("stale guard: %q %v", result, err)
	}
	// Host names are untrusted format data, including literal format expressions.
	for _, name := range []string{"space name", "a,b}x", "#{pid}", "#(false)", "quote'and\\slash"} {
		if result, err := run("tmux -L rename-catalog rename-session -t '$0' " + ShellQuote(strings.ReplaceAll(name, "#", "##"))); err != nil {
			t.Fatalf("set name %q: %s %v", name, result, err)
		}
		inv, out = take()
		req.Revision = inv.Sessions[0].RenameRevision
		command, err = RenameCommand("test", "rename-catalog", "$0", req, out)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := run(command); err != nil || string(result) != "MTM_RENAMED\n" {
			t.Fatalf("format name %q (%q): %q %v", name, inv.Sessions[0].Name, result, err)
		}
	}
}

func TestRenameValidation(t *testing.T) {
	r := RenameRequest{Name: "work_1", Epoch: "e_" + strings.Repeat("a", 64), Revision: "rn_" + strings.Repeat("b", 64), Confirmed: true}
	if !ValidRename("$0", r) {
		t.Fatal("valid rejected")
	}
	for _, name := range []string{"", "-x", "a.b", "a:b", "two words", "#{pid}", "$(false)", "a;kill-server", strings.Repeat("a", 65), "é"} {
		bad := r
		bad.Name = name
		if ValidRename("$0", bad) {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, id := range []string{"work", "$0;kill-server", "@0", "$-1"} {
		if ValidRename(id, r) {
			t.Fatal("invalid target accepted")
		}
	}
}
