package httpapi

import (
	"context"
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
	h.server.assets = webassets.Files()
	var probes atomic.Int32
	h.server.ssh = observerFunc(func(_ context.Context, host sshconnector.Host, _ sshconnector.Record) (tmux.Inventory, error) {
		if probes.Add(1) > 1 {
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
	go func() { _ = srv.Serve(listener) }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	t.Log("Browser fixture: http://localhost:18081; login admin with testPassword from server_test.go; first probe fresh, subsequent probes stale. No real SSH hosts are contacted.")
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Minute):
	}
}
