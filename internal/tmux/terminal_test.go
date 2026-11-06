package tmux

import (
	"strings"
	"testing"
)

const policyFixture = "F|123|1700000000|/tmp/fixture/default|3.8\nmanual\nT|$0|on|@0|120|39\nW|@0|manual|120|39\nW|@1|manual|90|30\nF|123|1700000000|/tmp/fixture/default|3.8\n"

func TestTerminalPolicyRequiresStableManualWindows(t *testing.T) {
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
	if !strings.Contains(command, "'read-only,ignore-size'") || strings.Contains(command, "-d") {
		t.Fatal("unsafe attach catalog")
	}
	if _, err := TerminalPolicyCommand("bad socket", "$0"); err == nil {
		t.Fatal("unsafe socket")
	}
}
