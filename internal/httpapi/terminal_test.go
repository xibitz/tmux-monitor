package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
)

type openerFunc func(context.Context, sshconnector.Host, sshconnector.Record, string, string) (sshconnector.Terminal, error)

func (f openerFunc) OpenTerminal(ctx context.Context, h sshconnector.Host, r sshconnector.Record, target, epoch string) (sshconnector.Terminal, error) {
	return f(ctx, h, r, target, epoch)
}

type burstTerminal struct{ closed atomic.Bool }

func (b *burstTerminal) Read(p []byte) (int, error) {
	if b.closed.Load() {
		return 0, io.EOF
	}
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
func (b *burstTerminal) Close() error                { b.closed.Store(true); return nil }
func (b *burstTerminal) Size() (int, int)            { return 120, 40 }
func (b *burstTerminal) Check(context.Context) error { return nil }

func TestTerminalBackpressureCapacityAndShutdown(t *testing.T) {
	t.Run("bounded queue", func(t *testing.T) {
		h, url, _ := terminalHarness(t)
		burst := &burstTerminal{}
		h.server.terminal = openerFunc(func(context.Context, sshconnector.Host, sshconnector.Record, string, string) (sshconnector.Terminal, error) {
			return burst, nil
		})
		conn, _, err := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		// Intentionally never read. The producer exceeds the fixed queue; no
		// indefinite buffer or offline replay is available to this viewer.
		deadline := time.Now().Add(5 * time.Second)
		for {
			var count int
			if err = h.db.QueryRow("SELECT count(*) FROM audit_events WHERE action='terminal.close' AND result='slow_consumer'").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("unbounded or stuck output consumer")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !burst.closed.Load() {
			t.Fatal("burst SSH stream not closed")
		}
	})
	t.Run("capacity and shutdown", func(t *testing.T) {
		h, url, opened := terminalHarness(t)
		var streams []*pipeTerminal
		for i := 0; i < 4; i++ {
			conn, _, err := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			streams = append(streams, <-opened)
			_, _ = readViewer(t, conn)
		}
		if conn, res, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin); e == nil || res.StatusCode != 429 {
			if conn != nil {
				_ = conn.CloseNow()
			}
			t.Fatal("viewer limit not enforced")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.server.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		for _, p := range streams {
			waitDetached(t, p)
		}
		h.server.viewMu.Lock()
		count := len(h.server.viewers)
		h.server.viewMu.Unlock()
		if count != 0 {
			t.Fatal("viewer reservations leaked")
		}
	})
}

type pipeTerminal struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	closed chan struct{}
	once   sync.Once
}

func newPipeTerminal() *pipeTerminal {
	r, w := io.Pipe()
	return &pipeTerminal{reader: r, writer: w, closed: make(chan struct{})}
}
func (p *pipeTerminal) Read(b []byte) (int, error) { return p.reader.Read(b) }
func (p *pipeTerminal) Close() error {
	p.once.Do(func() { _ = p.reader.Close(); _ = p.writer.Close(); close(p.closed) })
	return nil
}
func (p *pipeTerminal) Size() (int, int)            { return 120, 40 }
func (p *pipeTerminal) Check(context.Context) error { return nil }

const testEpoch = "e_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestTerminalDisabledAndScopedStyleNonce(t *testing.T) {
	h := setup(t)
	if r := h.request("GET", "/api/v1/terminal/options", "", true, false, ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"enabled":false`) {
		t.Fatal("terminal default must be disabled")
	}
	if r := h.request("POST", "/api/v1/terminal/tickets", `{}`, true, true, h.csrf); r.Code != 503 {
		t.Fatal("disabled terminal issued a ticket")
	}
	h.server.assets = fstest.MapFS{"index.html": {Data: []byte(`<meta name="csp-nonce" content="__CSP_NONCE__">`)}}
	first := h.request("GET", "/", "", false, false, "")
	second := h.request("GET", "/", "", false, false, "")
	policy := first.Header().Get("Content-Security-Policy")
	if strings.Contains(policy, "unsafe-inline") || !strings.Contains(policy, "style-src 'self' 'nonce-") || strings.Contains(first.Body.String(), "__CSP_NONCE__") || first.Body.String() == second.Body.String() {
		t.Fatal("invalid production nonce")
	}
	h.server.cfg.Development = true
	dev := h.request("GET", "/", "", false, false, "").Header().Get("Content-Security-Policy")
	if !strings.Contains(dev, "style-src 'self' 'unsafe-inline'") || strings.Contains(dev, "nonce-") {
		t.Fatal("Vite dev styles blocked by nonce")
	}
}

func terminalHarness(t *testing.T) (*harness, string, chan *pipeTerminal) {
	t.Helper()
	h := setup(t)
	seedSSH(t, h)
	h.server.cfg.TerminalEnabled = true
	h.server.cfg.AllowedSSHHosts["test.example.ts.net"] = true
	h.server.terminalInterval = 20 * time.Millisecond
	opened := make(chan *pipeTerminal, 8)
	h.server.terminal = openerFunc(func(_ context.Context, h sshconnector.Host, _ sshconnector.Record, target, epoch string) (sshconnector.Terminal, error) {
		if h.ID != "ssh-test" || target != "$0" || epoch != testEpoch {
			return nil, &sshconnector.Failure{Code: "terminal_target_invalid"}
		}
		p := newPipeTerminal()
		opened <- p
		return p, nil
	})
	srv := httptest.NewServer(h.server)
	t.Cleanup(srv.Close)
	return h, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/v1/terminal", opened
}
func issueTicket(t *testing.T, h *harness) string {
	t.Helper()
	r := h.request("POST", "/api/v1/terminal/tickets", `{"hostId":"ssh-test","sessionId":"$0","epoch":"`+testEpoch+`"}`, true, true, h.csrf)
	if r.Code != 201 {
		t.Fatalf("ticket %d %s", r.Code, r.Body)
	}
	var result struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Ticket
}
func dialViewer(t *testing.T, h *harness, url, ticket, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return websocket.Dial(ctx, url, &websocket.DialOptions{Host: strings.TrimPrefix(testOrigin, "https://"), HTTPHeader: http.Header{"Origin": []string{origin}, "Cookie": []string{h.cookie.String()}}, Subprotocols: []string{"tmux.v1", "ticket." + ticket}})
}
func readViewer(t *testing.T, conn *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	kind, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return kind, b
}
func waitDetached(t *testing.T, p *pipeTerminal) {
	t.Helper()
	select {
	case <-p.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("SSH stream not closed")
	}
}

func TestTerminalTicketAuthAndInputRejection(t *testing.T) {
	for _, input := range []struct {
		name string
		kind websocket.MessageType
		data string
	}{{"binary", websocket.MessageBinary, "private-input-never-audit"}, {"interactive", websocket.MessageText, `{"type":"terminal.set_mode","mode":"interactive"}`}, {"resize", websocket.MessageText, `{"type":"terminal.resize_request","cols":40,"rows":20}`}} {
		t.Run(input.name, func(t *testing.T) {
			h, url, opened := terminalHarness(t)
			body := `{"hostId":"ssh-test","sessionId":"$0","epoch":"` + testEpoch + `"}`
			if r := h.request("POST", "/api/v1/terminal/tickets", body, true, true, ""); r.Code != 403 {
				t.Fatal("ticket accepted without CSRF")
			}
			ticket := issueTicket(t, h)
			if conn, response, err := dialViewer(t, h, url, ticket, "https://wrong.example"); err == nil || response.StatusCode != 403 {
				if conn != nil {
					_ = conn.CloseNow()
				}
				t.Fatal("wrong Origin accepted")
			}
			conn, _, err := dialViewer(t, h, url, ticket, testOrigin)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if conn.Subprotocol() != "tmux.v1" {
				t.Fatal("secret ticket echoed as protocol")
			}
			p := <-opened
			_, data := readViewer(t, conn)
			var message struct{ Type, Mode, ViewerID string }
			if err := json.Unmarshal(data, &message); err != nil || message.Type != "terminal.opened" || message.Mode != "read_only" {
				t.Fatalf("open %s", data)
			}
			go func() { _, _ = io.WriteString(p.writer, "terminal-output-not-audit") }()
			kind, frame := readViewer(t, conn)
			id, _ := hex.DecodeString(message.ViewerID)
			if kind != websocket.MessageBinary || len(frame) < 30 || frame[0] != 1 || frame[1] != 0 || binary.BigEndian.Uint32(frame[2:6]) != 1 || binary.BigEndian.Uint64(frame[6:14]) != 1 || string(frame[14:30]) != string(id) || string(frame[30:]) != "terminal-output-not-audit" {
				t.Fatal("invalid frame")
			}
			if e := conn.Write(context.Background(), input.kind, []byte(input.data)); e != nil {
				t.Fatal(e)
			}
			_, data = readViewer(t, conn)
			if !strings.Contains(string(data), "input_forbidden") {
				t.Fatalf("input accepted: %s", data)
			}
			waitDetached(t, p)
			if replay, res, e := dialViewer(t, h, url, ticket, testOrigin); e == nil || res.StatusCode != 403 {
				if replay != nil {
					_ = replay.CloseNow()
				}
				t.Fatal("reused ticket accepted")
			}
			r := h.request("GET", "/api/v1/audit", "", true, false, "")
			if !strings.Contains(r.Body.String(), testEpoch+":$0") || !strings.Contains(r.Body.String(), "input_forbidden") {
				t.Fatal("terminal target audit absent")
			}
			for _, secret := range []string{ticket, "terminal-output-not-audit", "private-input-never-audit"} {
				if strings.Contains(r.Body.String(), secret) || strings.Contains(h.logs.String(), secret) {
					t.Fatal("terminal secret logged")
				}
			}
		})
	}
}

func TestTerminalViewerRevalidatesAccess(t *testing.T) {
	for _, mutation := range []struct{ name, query string }{{"logout", "DELETE FROM app_sessions"}, {"credential rotation", "UPDATE ssh_credentials SET key_ref='k_11111111111111111111111111111111'"}, {"host removal", "DELETE FROM hosts"}, {"ACL revoke", "DELETE FROM host_access"}} {
		t.Run(mutation.name, func(t *testing.T) {
			h, url, opened := terminalHarness(t)
			if mutation.name == "ACL revoke" {
				if _, e := h.db.Exec("UPDATE users SET is_admin=0"); e != nil {
					t.Fatal(e)
				}
				if _, e := h.db.Exec("INSERT INTO host_access(user_id,host_id,action) SELECT id,'ssh-test','tmux:read' FROM users"); e != nil {
					t.Fatal(e)
				}
			}
			conn, _, err := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			p := <-opened
			_, _ = readViewer(t, conn)
			if _, e := h.db.Exec(mutation.query); e != nil {
				t.Fatal(e)
			}
			_, data := readViewer(t, conn)
			if !strings.Contains(string(data), "access_revoked") {
				t.Fatalf("access not revoked: %s", data)
			}
			waitDetached(t, p)
		})
	}
}

func TestTerminalTicketBindingExpiryLimitsAndDetach(t *testing.T) {
	h, url, opened := terminalHarness(t)
	ticket := issueTicket(t, h)
	h.server.viewMu.Lock()
	expired := h.server.tickets[identity.TokenHash(ticket)]
	expired.Expires = time.Now().Add(-time.Second)
	h.server.tickets[identity.TokenHash(ticket)] = expired
	h.server.viewMu.Unlock()
	if conn, res, e := dialViewer(t, h, url, ticket, testOrigin); e == nil || res.StatusCode != 403 {
		if conn != nil {
			_ = conn.CloseNow()
		}
		t.Fatal("expired ticket accepted")
	}
	ticket = issueTicket(t, h)
	h.server.viewMu.Lock()
	bound := h.server.tickets[identity.TokenHash(ticket)]
	bound.TokenHash = "different-login"
	h.server.tickets[identity.TokenHash(ticket)] = bound
	h.server.viewMu.Unlock()
	if conn, res, e := dialViewer(t, h, url, ticket, testOrigin); e == nil || res.StatusCode != 403 {
		if conn != nil {
			_ = conn.CloseNow()
		}
		t.Fatal("different login accepted")
	}
	conn, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	p := <-opened
	_, _ = readViewer(t, conn)
	_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"terminal.detach"}`))
	_, data := readViewer(t, conn)
	if !strings.Contains(string(data), "detached") {
		t.Fatalf("detach %s", data)
	}
	waitDetached(t, p)
	for i := 0; i < 8; i++ {
		issueTicket(t, h)
	}
	if r := h.request("POST", "/api/v1/terminal/tickets", `{"hostId":"ssh-test","sessionId":"$0","epoch":"`+testEpoch+`"}`, true, true, h.csrf); r.Code != 429 {
		t.Fatal("ticket limit not enforced")
	}
}
