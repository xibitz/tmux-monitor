package tmux

import (
	"errors"
	"strings"
	"testing"
)

const fixture = "F|123|1700000000|/tmp/tmux-1000/default|3.8\nS\t$0\twork\\ space\t1700000001\nW\t$0\t@0\teditor\t0\t1\nP\t@0\t%0\t1\t120\t39\tzsh\nF|123|1700000000|/tmp/tmux-1000/default|3.8\n"

func TestParseSnapshotNativeIDsAndEpoch(t *testing.T) {
	inv, err := Parse("host", "default", "3.8", []byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Sessions[0].Name != "work space" || inv.Sessions[0].Windows[0].Panes[0].ID != "%0" || inv.Epoch == "" || inv.Sessions[0].Revision == "" {
		t.Fatalf("bad snapshot: %+v", inv)
	}
	if inv.Capabilities.PTYAttach || inv.Capabilities.ControlMode || !inv.Capabilities.ServerRunning {
		t.Fatal("unproven capability advertised")
	}
	other, err := Parse("host", "default", "3.8", []byte(strings.ReplaceAll(fixture, "1700000000", "1700001000")))
	if err != nil {
		t.Fatal(err)
	}
	if other.Epoch == inv.Epoch {
		t.Fatal("tmux restart did not change epoch")
	}
	again, err := Parse("host", "default", "3.8", []byte(fixture))
	if err != nil || again.Sessions[0].Revision != inv.Sessions[0].Revision {
		t.Fatal("revision not stable")
	}
	linked := strings.Replace(fixture, "W\t$0", "S\t$1\tsecond\t1700000002\nW\t$1\t@0\teditor\t1\t1\nW\t$0", 1)
	inv, err = Parse("host", "default", "3.8", []byte(linked))
	if err != nil || len(inv.Sessions) != 2 || len(inv.Sessions[1].Windows[0].Panes) != 1 {
		t.Fatalf("linked window failed: %v", err)
	}
}

func TestRejectMalformedAndChangedSnapshots(t *testing.T) {
	for name, output := range map[string]string{
		"duplicate session":   strings.Replace(fixture, "W\t$0", "S\t$0\tduplicate\t1700000001\nW\t$0", 1),
		"orphan window":       strings.Replace(fixture, "W\t$0", "W\t$8", 1),
		"wrong native id":     strings.Replace(fixture, "%0", "@0", 1),
		"negative width":      strings.Replace(fixture, "\t120\t", "\t-1\t", 1),
		"control name":        strings.Replace(fixture, "editor", "bad\x1bname", 1),
		"unknown record":      strings.Replace(fixture, "P\t@0", "X\t@0", 1),
		"missing fields":      strings.Replace(fixture, "editor\t0\t1", "editor", 1),
		"invalid fingerprint": strings.ReplaceAll(fixture, "|123|", "|0|"),
		"raw newline name":    strings.Replace(fixture, "editor", "bad\nname", 1),
		"empty":               "",
		"output limit":        strings.Repeat("x", MaxOutput+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("h", "default", "3.8", []byte(output)); err == nil {
				t.Fatal("unsafe output accepted")
			}
		})
	}
	changed := strings.TrimSuffix(fixture, "F|123|1700000000|/tmp/tmux-1000/default|3.8\n") + "F|124|1700000000|/tmp/tmux-1000/default|3.8\n"
	if _, err := Parse("h", "default", "3.8", []byte(changed)); !errors.Is(err, ErrChanged) {
		t.Fatalf("epoch change: %v", err)
	}
}

func TestCatalogRejectsInjectionAndUnquotesData(t *testing.T) {
	for _, socket := range []string{"-Lother", "x;id", "../../x", "a\nkill-server", "$(id)"} {
		if _, err := SnapshotCommand(socket); err == nil {
			t.Fatal("socket injection accepted")
		}
	}
	command, err := SnapshotCommand("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []string{"kill-", "new-session", "run-shell", "send-keys", "#{E:"} {
		if strings.Contains(command, blocked) {
			t.Fatalf("mutating catalog: %s", blocked)
		}
	}
	if got, err := unquote("日本語\\ ⚙️\\'\\$\\;"); err != nil || got != "日本語 ⚙️'$;" {
		t.Fatalf("escaped metadata: %s %v", got, err)
	}
	if ShellQuote("a'b") != "'a'\"'\"'b'" {
		t.Fatal("incorrect SSH shell serialization")
	}
}

func FuzzParseSnapshot(f *testing.F) {
	f.Add([]byte(fixture))
	f.Add([]byte("F|0\n"))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = Parse("test", "default", "3.8", b) })
}
