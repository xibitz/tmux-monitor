package config

import "testing"

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
