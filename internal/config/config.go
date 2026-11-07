package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Listen                  string
	DataDir                 string
	Origin                  string
	Development             bool
	TerminalEnabled         bool
	MutationsEnabled        bool
	SessionLifecycleEnabled bool
	AllowedSSHHosts         map[string]bool
}

func Load() (Config, error) {
	c := Config{Listen: env("TMUX_MONITOR_LISTEN", "127.0.0.1:8080"), DataDir: env("TMUX_MONITOR_DATA_DIR", ".data"), Origin: os.Getenv("TMUX_MONITOR_ORIGIN"), AllowedSSHHosts: map[string]bool{}}
	var err error
	c.Development, err = strconv.ParseBool(env("TMUX_MONITOR_DEV", "false"))
	if err != nil {
		return c, fmt.Errorf("TMUX_MONITOR_DEV must be true or false")
	}
	c.TerminalEnabled, err = strconv.ParseBool(env("TMUX_MONITOR_TERMINAL_ENABLED", "false"))
	if err != nil {
		return c, fmt.Errorf("TMUX_MONITOR_TERMINAL_ENABLED must be true or false")
	}
	for _, host := range strings.Split(os.Getenv("TMUX_MONITOR_ALLOWED_SSH_HOSTS"), ",") {
		if host = strings.TrimSpace(strings.ToLower(host)); host != "" {
			c.AllowedSSHHosts[host] = true
		}
	}
	c.MutationsEnabled, err = strconv.ParseBool(env("TMUX_MONITOR_MUTATIONS_ENABLED", "false"))
	if err != nil {
		return c, fmt.Errorf("TMUX_MONITOR_MUTATIONS_ENABLED must be true or false")
	}
	c.SessionLifecycleEnabled, err = strconv.ParseBool(env("TMUX_MONITOR_SESSION_LIFECYCLE_ENABLED", "false"))
	if err != nil {
		return c, fmt.Errorf("TMUX_MONITOR_SESSION_LIFECYCLE_ENABLED must be true or false")
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("listen address must be 127.0.0.1:<port>")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid listen port")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return fmt.Errorf("TMUX_MONITOR_ORIGIN must be an exact origin without path, credentials, query, or fragment")
	}
	if c.Development {
		if u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
			return fmt.Errorf("development origin must be HTTP on localhost or 127.0.0.1")
		}
	} else if u.Scheme != "https" {
		return fmt.Errorf("production origin must use HTTPS")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data directory must not be empty")
	}
	return nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
