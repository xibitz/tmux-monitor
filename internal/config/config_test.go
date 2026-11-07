package config

import "testing"

func TestMutationsRequireExplicitOptIn(t *testing.T) {
	t.Setenv("TMUX_MONITOR_ORIGIN", "https://hub.example.ts.net")
	t.Setenv("TMUX_MONITOR_DEV", "false")
	t.Setenv("TMUX_MONITOR_TERMINAL_ENABLED", "false")
	for _, value := range []string{"", "true", "invalid"} {
		t.Setenv("TMUX_MONITOR_MUTATIONS_ENABLED", value)
		c, err := Load()
		if value == "invalid" {
			if err == nil {
				t.Fatal("invalid mutation gate accepted")
			}
			continue
		}
		if err != nil || c.MutationsEnabled != (value == "true") {
			t.Fatal("mutation opt-in invalid")
		}
	}
}

func TestTerminalRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("TMUX_MONITOR_ORIGIN", "https://hub.example.ts.net")
	t.Setenv("TMUX_MONITOR_DEV", "false")
	t.Setenv("TMUX_MONITOR_TERMINAL_ENABLED", "")
	c, err := Load()
	if err != nil || c.TerminalEnabled {
		t.Fatal("terminal must default off")
	}
	t.Setenv("TMUX_MONITOR_TERMINAL_ENABLED", "true")
	c, err = Load()
	if err != nil || !c.TerminalEnabled {
		t.Fatal("explicit opt-in failed")
	}
	t.Setenv("TMUX_MONITOR_TERMINAL_ENABLED", "invalid")
	if _, err = Load(); err == nil {
		t.Fatal("invalid feature setting accepted")
	}
}

func TestNetworkBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, listen, origin string
		dev, valid           bool
	}{
		{"production", "127.0.0.1:8080", "https://hub.example.ts.net", false, true},
		{"public listener", "0.0.0.0:8080", "https://hub.example.ts.net", false, false},
		{"hostname listener", "localhost:8080", "https://hub.example.ts.net", false, false},
		{"http production", "127.0.0.1:8080", "http://localhost:8080", false, false},
		{"local development", "127.0.0.1:8080", "http://127.0.0.1:5173", true, true},
		{"remote development", "127.0.0.1:8080", "http://example.com", true, false},
		{"origin path", "127.0.0.1:8080", "https://hub.example.ts.net/path", false, false},
		{"origin userinfo", "127.0.0.1:8080", "https://user@hub.example.ts.net", false, false},
		{"invalid port", "127.0.0.1:99999", "https://hub.example.ts.net", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Listen: tc.listen, Origin: tc.origin, Development: tc.dev, DataDir: t.TempDir()}
			if (c.Validate() == nil) != tc.valid {
				t.Fatalf("unexpected validation result: %v", c.Validate())
			}
		})
	}
}
