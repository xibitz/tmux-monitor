package tmux

import (
	"strings"
	"testing"
)

const policyFixture = "F|123|1700000000|/tmp/fixture/default|3.8\nmanual\nT|$0|on|@0|120|39\nW|@0|manual|120|39\nW|@1|manual|90|30\nF|123|1700000000|/tmp/fixture/default|3.8\noff\noff\n"

func TestTerminalPolicyRequiresStableManualWindows(t *testing.T) {
	for _, version := range []string{"3.4", "3.6", "3.6a", "3.6b"} {
		output := strings.ReplaceAll(policyFixture, "3.8", version)
		epoch := digest("e_", "host", "default", strings.Split(output, "\n")[0])
		if _, err := ParseTerminalPolicy("host", "default", "$0", epoch, []byte(output)); err != ErrInvalid {
			t.Fatalf("unsupported server %s accepted: %v", version, err)
		}
	}
	for _, version := range []string{"3.7", "3.7a", "3.8", "4.0"} {
		if !TerminalVersionSupported(version) {
			t.Fatalf("supported version rejected: %s", version)
		}
	}
	epoch := digest("e_", "host", "default", strings.Split(policyFixture, "\n")[0])
	policy, err := ParseTerminalPolicy("host", "default", "$0", epoch, []byte(policyFixture))
	if err != nil || policy.Cols != 120 || policy.Rows != 40 {
		t.Fatalf("policy: %+v %v", policy, err)
	}
	for _, output := range []string{
		strings.Replace(policyFixture, "\nmanual\n", "\nlatest\n", 1),
		strings.Replace(policyFixture, "W|@1|manual", "W|@1|smallest", 1),
		strings.Replace(policyFixture, "T|$0", "T|$1", 1),
		strings.Replace(policyFixture, "T|$0|on|@0|120", "T|$0|on|@0|1000", 1),
		strings.Replace(policyFixture, "W|@0|manual|120", "W|@0|manual|90", 1),
		strings.Replace(policyFixture, "W|@1", "W|@0", 1),
		strings.Replace(policyFixture, "F|123", "F|124", 1),
	} {
		if _, err := ParseTerminalPolicy("host", "default", "$0", epoch, []byte(output)); err == nil {
			t.Fatalf("unsafe policy accepted: %q", output)
		}
	}
	for _, target := range []string{"work", "$0; kill-server", "@0", "$0\n"} {
		if _, err := AttachCommand("default", target); err == nil {
			t.Fatal("unsafe target")
		}
	}
	command, _ := AttachCommand("default", "$0")
	if !strings.Contains(command, "read-only,ignore-size") || strings.Contains(command, "-d") {
		t.Fatal("unsafe attach catalog")
	}
	if _, err := TerminalPolicyCommand("bad socket", "$0"); err == nil {
		t.Fatal("unsafe socket")
	}
	if err := CheckClientTarget([]byte("C|/dev/pts/4|$0\nC|/dev/pts/5|$1\n"), "/dev/pts/4", "$0"); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{"C|/dev/pts/4|$1\n", "C|/dev/pts/5|$0\n", "C|/dev/pts/4|$0\nC|/dev/pts/4|$0\n", "bad"} {
		if CheckClientTarget([]byte(out), "/dev/pts/4", "$0") == nil {
			t.Fatal("bad client target accepted")
		}
	}
	if ValidTTY("/dev/pts/4; rm -rf /") {
		t.Fatal("unsafe tty")
	}
}
