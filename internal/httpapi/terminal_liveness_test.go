package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
)

func dialLiveViewer(t *testing.T, h *harness, url, ticket string, pong bool) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return websocket.Dial(ctx, url, &websocket.DialOptions{
		Host:           strings.TrimPrefix(testOrigin, "https://"),
		HTTPHeader:     http.Header{"Origin": []string{testOrigin}, "Cookie": []string{h.cookie.String()}},
		Subprotocols:   []string{"tmux.v2", "ticket." + ticket},
		OnPingReceived: func(context.Context, []byte) bool { return pong },
	})
}

func readLiveControl(t *testing.T, c *websocket.Conn, viewer, want string) []byte {
	t.Helper()
	for attempts := 0; attempts < 64; attempts++ {
		kind, data := readViewer(t, c)
		if kind != websocket.MessageText {
			t.Fatalf("unexpected binary while waiting for %s", want)
		}
		var message struct {
			Type, ViewerID string
			Generation     int
		}
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.Type == "terminal.heartbeat" {
			if message.ViewerID != viewer || message.Generation != 1 {
				t.Fatalf("unbound heartbeat: %s", data)
			}
			if want != message.Type {
				continue
			}
		}
		if message.Type != want {
			t.Fatalf("got %s, wanted %s", data, want)
		}
		return data
	}
	t.Fatalf("too many heartbeats while waiting for %s", want)
	return nil
}

func TestTerminalLivenessIdleAndLegacy(t *testing.T) {
	for _, version := range []string{"tmux.v1", "tmux.v2"} {
		t.Run(version, func(t *testing.T) {
			h, url, opened := terminalHarness(t)
			h.server.terminalPingInterval = 20 * time.Millisecond
			h.server.terminalPingTimeout = 500 * time.Millisecond
			var c *websocket.Conn
			var err error
			if version == "tmux.v2" {
				c, _, err = dialLiveViewer(t, h, url, issueTicket(t, h), true)
			} else {
				c, _, err = dialViewer(t, h, url, issueTicket(t, h), testOrigin)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseNow()
			if c.Subprotocol() != version {
				t.Fatalf("negotiated %s", c.Subprotocol())
			}
			p := <-opened
			m := openViewerMessage(t, c)
			if version == "tmux.v2" {
				// No PTY output: healthy idle sessions must not time out.
				for i := 0; i < 3; i++ {
					readLiveControl(t, c, m.ViewerID, "terminal.heartbeat")
				}
			} else {
				// v1 must not receive new JSON control messages.
				time.Sleep(70 * time.Millisecond)
			}
			for sequence := uint64(1); sequence <= 2; sequence++ {
				go func() { _, _ = io.WriteString(p.writer, "idle-output") }()
				for {
					kind, data := readViewer(t, c)
					if kind == websocket.MessageText && version == "tmux.v2" {
						if !strings.Contains(string(data), "terminal.heartbeat") {
							t.Fatalf("unexpected control %s", data)
						}
						continue
					}
					if kind != websocket.MessageBinary || len(data) != 41 || binary.BigEndian.Uint64(data[6:14]) != sequence || string(data[30:]) != "idle-output" {
						t.Fatalf("heartbeat corrupted binary sequence: %q", data)
					}
					break
				}
				if version == "tmux.v2" {
					readLiveControl(t, c, m.ViewerID, "terminal.heartbeat")
				}
			}
			// Heartbeat support never grants read-only viewers a write path.
			if err = c.Write(context.Background(), websocket.MessageBinary, inputFrame(m.ViewerID, 1, "must-not-write")); err != nil {
				t.Fatal(err)
			}
			data := readLiveControl(t, c, m.ViewerID, "terminal.closed")
			if !strings.Contains(string(data), "input_forbidden") {
				t.Fatalf("read-only input accepted: %s", data)
			}
			waitDetached(t, p)
		})
	}
}

func TestTerminalLivenessMissingPongAndFreshReopen(t *testing.T) {
	h, url, opened := terminalHarness(t)
	h.server.terminalPingInterval = 20 * time.Millisecond
	h.server.terminalPingTimeout = 100 * time.Millisecond
	ticket := issueTicket(t, h)
	c, _, err := dialLiveViewer(t, h, url, ticket, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	p := <-opened
	m := openViewerMessage(t, c)
	data := readLiveControl(t, c, m.ViewerID, "terminal.closed")
	if !strings.Contains(string(data), "heartbeat_timeout") {
		t.Fatalf("missing pong did not detach: %s", data)
	}
	waitDetached(t, p)
	if reused, res, err := dialLiveViewer(t, h, url, ticket, true); err == nil || res.StatusCode != 403 {
		if reused != nil {
			_ = reused.CloseNow()
		}
		t.Fatal("reconnect reused consumed ticket")
	}
	fresh, _, err := dialLiveViewer(t, h, url, issueTicket(t, h), true)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.CloseNow()
	<-opened
	next := openViewerMessage(t, fresh)
	if next.ViewerID == m.ViewerID || next.Mode != "read_only" {
		t.Fatal("reopen retained old identity or mode")
	}
	readLiveControl(t, fresh, next.ViewerID, "terminal.heartbeat")
	var count int
	deadline := time.Now().Add(time.Second)
	for {
		if err = h.db.QueryRow("SELECT count(*) FROM audit_events WHERE action='terminal.close' AND result='heartbeat_timeout'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing timeout audit: %d", count)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTerminalHubRestartDropsTicketsAndInteractiveInput(t *testing.T) {
	h, url, opened := terminalHarness(t)
	h.server.terminalPingInterval = 20 * time.Millisecond
	h.server.terminalPingTimeout = 500 * time.Millisecond
	interactive := enableInteractive(h)
	c, _, err := dialLiveViewer(t, h, url, issueTicket(t, h), true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	<-opened
	m := openViewerMessage(t, c)
	ticket := modeTicket(t, h, m.ViewerID, "interactive", 201)
	ic, _, err := dialLiveViewer(t, h, url, ticket, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ic.CloseNow()
	p := <-interactive
	im := openViewerMessage(t, ic)
	readLiveControl(t, ic, im.ViewerID, "terminal.heartbeat")
	if len(p.input) != 0 {
		t.Fatal("heartbeat was written to SSH stdin")
	}
	if err = ic.Write(context.Background(), websocket.MessageBinary, inputFrame(im.ViewerID, 1, "test-only-input")); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-p.input:
		if string(data) != "test-only-input" {
			t.Fatal("wrong input")
		}
	case <-time.After(time.Second):
		t.Fatal("interactive input not delivered")
	}
	readLiveControl(t, ic, im.ViewerID, "terminal.heartbeat")
	if err = ic.Write(context.Background(), websocket.MessageBinary, inputFrame(im.ViewerID, 2, "second-test-input")); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-p.input:
		if string(data) != "second-test-input" {
			t.Fatal("heartbeat corrupted input sequence")
		}
	case <-time.After(time.Second):
		t.Fatal("input after heartbeat not delivered")
	}
	unused := issueTicket(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = h.server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	closed := readLiveControl(t, ic, im.ViewerID, "terminal.closed")
	if !strings.Contains(string(closed), "hub_shutdown") {
		t.Fatalf("shutdown reason lost: %s", closed)
	}
	waitDetached(t, p.pipeTerminal)

	// A new hub instance shares persistent login/host data, but no stream,
	// pending ticket, mode grant, or input queue from the previous process.
	previous := h.server
	h.server = New(h.db, previous.cfg, previous.assets, previous.log)
	newStreams := make(chan *pipeTerminal, 1)
	h.server.terminal = openerFunc(func(context.Context, sshconnector.Host, sshconnector.Record, string, string) (sshconnector.Terminal, error) {
		p := newPipeTerminal()
		newStreams <- p
		return p, nil
	})
	srv := httptest.NewServer(h.server)
	t.Cleanup(srv.Close)
	url = "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/v1/terminal"
	if stale, res, err := dialLiveViewer(t, h, url, unused, true); err == nil || res.StatusCode != 403 {
		if stale != nil {
			_ = stale.CloseNow()
		}
		t.Fatal("old hub ticket survived restart")
	}
	fresh, _, err := dialLiveViewer(t, h, url, issueTicket(t, h), true)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.CloseNow()
	newStream := <-newStreams
	next := openViewerMessage(t, fresh)
	if next.ViewerID == im.ViewerID || next.Mode != "read_only" {
		t.Fatal("restart restored old mode or identity")
	}
	if len(p.input) != 0 {
		t.Fatal("old input queued for replay")
	}
	if err = fresh.Write(context.Background(), websocket.MessageBinary, inputFrame(next.ViewerID, 1, "new-readonly-input")); err != nil {
		t.Fatal(err)
	}
	closed = readLiveControl(t, fresh, next.ViewerID, "terminal.closed")
	if !strings.Contains(string(closed), "input_forbidden") {
		t.Fatal("new hub allowed old interactive authority")
	}
	waitDetached(t, newStream)
}
