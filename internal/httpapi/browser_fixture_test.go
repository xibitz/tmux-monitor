package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/tmux"
	"github.com/xibitz/tmux-monitor/internal/webassets"
)

type browserInteractiveOpener struct{ openerFunc }

func (o browserInteractiveOpener) OpenInteractiveTerminal(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _, _ string) (sshconnector.Terminal, error) {
	p := &writablePipe{pipeTerminal: newPipeTerminal(), input: make(chan []byte, 16)}
	go func() {
		_, _ = io.WriteString(p.writer, "\x1b[2J\x1b[H\x1b[32mFixture terminal interaktif\x1b[0m\r\nInput di bawah hanya echo fixture UI.\r\nSSH/PTY nyata diuji pada socket test terpisah.\r\n\r\n> ")
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.closed:
				return
			case data := <-p.input:
				if string(data) == "\x03" {
					data = []byte("^C\r\n> ")
				} else {
					data = bytes.ReplaceAll(data, []byte("\r"), []byte("\r\n"))
				}
				if _, err := p.writer.Write(data); err != nil {
					return
				}
			}
		}
	}()
	return p, nil
}

// Explicit manual browser fixture, excluded from shipped binaries. All HTTP,
// login/CSRF and persistence are real; only SSH observation data is simulated.
func TestBrowserFixture(t *testing.T) {
	if os.Getenv("TMUX_MONITOR_BROWSER_FIXTURE") != "1" {
		t.Skip("manual browser fixture")
	}
	h := setup(t)
	seedSSH(t, h)
	if _, err := h.db.Exec("UPDATE hosts SET label='Host uji SSH' WHERE id='ssh-test'"); err != nil {
		t.Fatal(err)
	}
	h.server.cfg.Listen = "127.0.0.1:18081"
	h.server.cfg.Origin = "http://localhost:18081"
	h.server.cfg.Development = true
	terminalMode := os.Getenv("TMUX_MONITOR_BROWSER_TERMINAL") == "1"
	if terminalMode {
		h.server.cfg.TerminalEnabled = true
		h.server.cfg.Development = false // production CSP, Secure cookie on localhost
		h.server.cfg.AllowedSSHHosts["test.example.ts.net"] = true
		h.server.terminal = browserInteractiveOpener{openerFunc(func(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _, _ string) (sshconnector.Terminal, error) {
			p := newPipeTerminal()
			go func() {
				_, _ = io.WriteString(p.writer, "\x1b[2J\x1b[H\x1b[32mtmux monitor · fixture read-only\x1b[0m\r\n\r\nHost uji / session-untuk-uji\r\nPTY 120 x 40 · ukuran remote dipertahankan\r\n\r\nOutput ini disimulasikan untuk pengujian UI.\r\nSSH dengan PTY dan tmux nyata diuji terpisah.\r\n\r\n\x1b]52;c;dGVzdA==\x07\x1b]8;;https://example.invalid\x07Tautan terminal tidak diaktifkan\x1b]8;;\x07\r\n")
				tick := time.NewTicker(time.Second)
				defer tick.Stop()
				for n := 1; ; n++ {
					select {
					case <-ctx.Done():
						return
					case <-p.closed:
						return
					case <-tick.C:
						if n > 60 {
							_ = p.Close()
							return
						}
						if _, err := fmt.Fprintf(p.writer, "\r\nheartbeat fixture %d", n); err != nil {
							return
						}
					}
				}
			}()
			return p, nil
		})}
	}
	h.server.assets = webassets.Files()
	var probes atomic.Int32
	h.server.ssh = observerFunc(func(_ context.Context, host sshconnector.Host, _ sshconnector.Record) (tmux.Inventory, error) {
		if probes.Add(1) > 1 && !terminalMode {
			return tmux.Inventory{}, &sshconnector.Failure{Code: "timeout"}
		}
		return tmux.Parse(host.ID, host.Socket, "3.8", []byte("F|123|1700000000|/tmp/fixture/default|3.8\nS\t$0\tsession-untuk-uji\t1700000001\nW\t$0\t@0\teditor\t0\t1\nP\t@0\t%0\t1\t120\t39\tzsh\nF|123|1700000000|/tmp/fixture/default|3.8\n"))
	})
	listener, err := net.Listen("tcp", h.server.cfg.Listen)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h.server, ReadHeaderTimeout: 5 * time.Second}
	defer srv.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = h.server.Shutdown(ctx)
	}()
	go func() { _ = srv.Serve(listener) }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	t.Log("Browser fixture: http://localhost:18081; login admin with testPassword from server_test.go. SSH/terminal output simulated; production CSP when terminal mode is enabled.")
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Minute):
	}
}
