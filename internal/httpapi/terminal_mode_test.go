package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
)

type writablePipe struct {
	*pipeTerminal
	input      chan []byte
	writeError error
}

func (p *writablePipe) WriteInput(ctx context.Context, b []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.input <- append([]byte(nil), b...):
		return p.writeError
	}
}

type writableOpener struct {
	openerFunc
	opened chan *writablePipe
}

func (o writableOpener) OpenInteractiveTerminal(_ context.Context, _ sshconnector.Host, _ sshconnector.Record, _, _ string) (sshconnector.Terminal, error) {
	p := &writablePipe{pipeTerminal: newPipeTerminal(), input: make(chan []byte, 16)}
	o.opened <- p
	return p, nil
}

func enableInteractive(h *harness) chan *writablePipe {
	opened := make(chan *writablePipe, 8)
	ro := h.server.terminal
	h.server.terminal = writableOpener{openerFunc: openerFunc(ro.OpenTerminal), opened: opened}
	return opened
}

type openedMessage struct {
	Type, Mode, ViewerID string
	CanWrite             bool
}

func openViewerMessage(t *testing.T, c *websocket.Conn) openedMessage {
	t.Helper()
	_, b := readViewer(t, c)
	var m openedMessage
	if json.Unmarshal(b, &m) != nil || m.Type != "terminal.opened" {
		t.Fatalf("not opened: %s", b)
	}
	return m
}
func modeTicket(t *testing.T, h *harness, viewer, mode string, want int) string {
	t.Helper()
	r := h.request("POST", "/api/v1/terminal/viewers/"+viewer+"/mode-tickets", `{"mode":"`+mode+`"}`, true, true, h.csrf)
	if r.Code != want {
		t.Fatalf("mode %d want %d: %s", r.Code, want, r.Body)
	}
	if want != 201 {
		return ""
	}
	var m struct {
		Ticket    string
		ExpiresIn int
	}
	if json.Unmarshal(r.Body.Bytes(), &m) != nil || m.ExpiresIn != 15 {
		t.Fatal("invalid mode grant")
	}
	return m.Ticket
}
func inputFrame(viewer string, seq uint64, text string) []byte {
	id, _ := hex.DecodeString(viewer)
	b := make([]byte, 30+len(text))
	b[0] = 1
	b[1] = 1
	binary.BigEndian.PutUint32(b[2:6], 1)
	binary.BigEndian.PutUint64(b[6:14], seq)
	copy(b[14:30], id)
	copy(b[30:], text)
	return b
}

func TestInteractiveModeRequiresIntentAndResetsOnReopen(t *testing.T) {
	h, url, readOpened := terminalHarness(t)
	writeOpened := enableInteractive(h)
	c, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	ro := <-readOpened
	m := openViewerMessage(t, c)
	if !m.CanWrite || m.Mode != "read_only" {
		t.Fatal("ordinary ticket must be read-only with explicit write capability")
	}
	side, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer side.CloseNow()
	sideStream := <-readOpened
	openViewerMessage(t, side)
	path := "/api/v1/terminal/viewers/" + m.ViewerID + "/mode-tickets"
	if r := h.request("POST", path, `{"mode":"interactive"}`, true, true, ""); r.Code != 403 {
		t.Fatal("CSRF missing accepted")
	}
	if r := h.request("POST", path, `{"mode":"interactive"}`, true, false, h.csrf); r.Code != 403 {
		t.Fatal("Origin missing accepted")
	}
	ticket := modeTicket(t, h, m.ViewerID, "interactive", 201)
	waitDetached(t, ro)
	select {
	case <-sideStream.closed:
		t.Fatal("mode change detached another viewer")
	default:
	}
	_, closed := readViewer(t, c)
	if !strings.Contains(string(closed), "mode_changed") {
		t.Fatal("old viewer not retired")
	}
	modeTicket(t, h, m.ViewerID, "interactive", 404)
	ic, _, e := dialViewer(t, h, url, ticket, testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer ic.CloseNow()
	p := <-writeOpened
	im := openViewerMessage(t, ic)
	if im.Mode != "interactive" || im.ViewerID == m.ViewerID {
		t.Fatal("mode change must create a new viewer")
	}
	text := "uji-秘密-é\t\r\x03\x1b[A"
	if e = ic.Write(context.Background(), websocket.MessageBinary, inputFrame(im.ViewerID, 1, text)); e != nil {
		t.Fatal(e)
	}
	select {
	case b := <-p.input:
		if string(b) != text {
			t.Fatal("input bytes changed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("input absent")
	}
	back := modeTicket(t, h, im.ViewerID, "read_only", 201)
	waitDetached(t, p.pipeTerminal)
	bc, _, e := dialViewer(t, h, url, back, testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer bc.CloseNow()
	next := <-readOpened
	bm := openViewerMessage(t, bc)
	if bm.Mode != "read_only" {
		t.Fatal("downgrade not read-only")
	}
	_ = bc.Write(context.Background(), websocket.MessageBinary, inputFrame(bm.ViewerID, 1, "denied"))
	_, closed = readViewer(t, bc)
	if !strings.Contains(string(closed), "input_forbidden") {
		t.Fatal("readonly input allowed")
	}
	waitDetached(t, next)
	fresh, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.CloseNow()
	<-readOpened
	if openViewerMessage(t, fresh).Mode != "read_only" {
		t.Fatal("reopen inherited interactive")
	}
	if replay, res, e := dialViewer(t, h, url, ticket, testOrigin); e == nil || res.StatusCode != 403 {
		if replay != nil {
			_ = replay.CloseNow()
		}
		t.Fatal("mode ticket reused")
	}
	r := h.request("GET", "/api/v1/audit", "", true, false, "")
	if !strings.Contains(r.Body.String(), "requested_interactive") || strings.Contains(r.Body.String(), text) || strings.Contains(h.logs.String(), text) || strings.Contains(r.Body.String(), ticket) {
		t.Fatal("invalid/redacted mode audit")
	}
}

func TestInteractivePermissionIsCheckedForGrantAndEveryFrame(t *testing.T) {
	h, url, readOpened := terminalHarness(t)
	writeOpened := enableInteractive(h)
	h.server.terminalInterval = time.Hour
	if _, e := h.db.Exec("UPDATE users SET is_admin=0"); e != nil {
		t.Fatal(e)
	}
	if _, e := h.db.Exec("INSERT INTO host_access(user_id,host_id,action) SELECT id,'ssh-test','tmux:read' FROM users"); e != nil {
		t.Fatal(e)
	}
	c, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	<-readOpened
	m := openViewerMessage(t, c)
	if m.CanWrite {
		t.Fatal("tmux:read implies write")
	}
	modeTicket(t, h, m.ViewerID, "interactive", 403)
	if _, e := h.db.Exec("INSERT INTO host_access(user_id,host_id,action) SELECT id,'ssh-test','terminal:write' FROM users"); e != nil {
		t.Fatal(e)
	}
	ticket := modeTicket(t, h, m.ViewerID, "interactive", 201)
	ic, _, e := dialViewer(t, h, url, ticket, testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer ic.CloseNow()
	p := <-writeOpened
	im := openViewerMessage(t, ic)
	if _, e := h.db.Exec("DELETE FROM host_access WHERE action='terminal:write'"); e != nil {
		t.Fatal(e)
	}
	_ = ic.Write(context.Background(), websocket.MessageBinary, inputFrame(im.ViewerID, 1, "must-not-forward"))
	_, closed := readViewer(t, ic)
	if !strings.Contains(string(closed), "access_revoked") {
		t.Fatalf("revoked input accepted: %s", closed)
	}
	waitDetached(t, p.pipeTerminal)
	select {
	case <-p.input:
		t.Fatal("bytes sent after revoke")
	default:
	}
}

func TestInteractiveFrameBindingReplayAndLimits(t *testing.T) {
	for _, test := range []struct {
		name   string
		edit   func([]byte) []byte
		reason string
	}{
		{"viewer", func(b []byte) []byte { b[14] ^= 1; return b }, "input_invalid"},
		{"generation", func(b []byte) []byte { b[5] = 2; return b }, "input_invalid"},
		{"direction", func(b []byte) []byte { b[1] = 0; return b }, "input_invalid"},
		{"sequence gap", func(b []byte) []byte { binary.BigEndian.PutUint64(b[6:14], 2); return b }, "input_invalid"},
		{"oversize", func(b []byte) []byte { return append(b, make([]byte, 16384)...) }, "input_limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, url, ro := terminalHarness(t)
			wo := enableInteractive(h)
			c, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
			if e != nil {
				t.Fatal(e)
			}
			defer c.CloseNow()
			<-ro
			m := openViewerMessage(t, c)
			ic, _, e := dialViewer(t, h, url, modeTicket(t, h, m.ViewerID, "interactive", 201), testOrigin)
			if e != nil {
				t.Fatal(e)
			}
			defer ic.CloseNow()
			p := <-wo
			im := openViewerMessage(t, ic)
			_ = ic.Write(context.Background(), websocket.MessageBinary, test.edit(inputFrame(im.ViewerID, 1, "x")))
			_, b := readViewer(t, ic)
			if !strings.Contains(string(b), test.reason) {
				t.Fatalf("bad frame: %s", b)
			}
			waitDetached(t, p.pipeTerminal)
			select {
			case <-p.input:
				t.Fatal("invalid input forwarded")
			default:
			}
		})
	}
}

// A caller cannot select interactive on the ordinary ticket endpoint.
func TestOrdinaryTicketRejectsModeInjection(t *testing.T) {
	h := setup(t)
	h.server.cfg.TerminalEnabled = true
	r := h.request("POST", "/api/v1/terminal/tickets", `{"hostId":"ssh-test","sessionId":"$0","epoch":"`+testEpoch+`","mode":"interactive"}`, true, true, h.csrf)
	if r.Code != 400 {
		t.Fatal("ordinary mode injection accepted")
	}
}

func TestModeTicketIsLoginBoundAndOneTransitionOnly(t *testing.T) {
	h, url, ro := terminalHarness(t)
	enableInteractive(h)
	c, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	p := <-ro
	m := openViewerMessage(t, c)
	_, other, e := identity.Login(context.Background(), h.db, "admin", testPassword)
	if e != nil {
		t.Fatal(e)
	}
	originalCookie, originalCSRF := h.cookie, h.csrf
	copyCookie := *h.cookie
	copyCookie.Value = other
	h.cookie = &copyCookie
	h.csrf = identity.CSRF(other)
	modeTicket(t, h, m.ViewerID, "interactive", 404)
	select {
	case <-p.closed:
		t.Fatal("another login closed viewer")
	default:
	}
	h.cookie, h.csrf = originalCookie, originalCSRF
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r := h.request("POST", "/api/v1/terminal/viewers/"+m.ViewerID+"/mode-tickets", `{"mode":"interactive"}`, true, true, h.csrf)
			results <- r.Code
		}()
	}
	first, second := <-results, <-results
	if (first == 201) == (second == 201) {
		t.Fatalf("expected exactly one transition: %d %d", first, second)
	}
	for _, code := range []int{first, second} {
		if code != 201 && code != 409 && code != 404 {
			t.Fatalf("unexpected transition response %d", code)
		}
	}
	waitDetached(t, p)
}

func TestInteractiveSequenceReplayAndFailedWriteAreNotRetried(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "replay", true: "failed write"}[failWrite], func(t *testing.T) {
			h, url, ro := terminalHarness(t)
			wo := enableInteractive(h)
			c, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
			if e != nil {
				t.Fatal(e)
			}
			defer c.CloseNow()
			<-ro
			m := openViewerMessage(t, c)
			ic, _, e := dialViewer(t, h, url, modeTicket(t, h, m.ViewerID, "interactive", 201), testOrigin)
			if e != nil {
				t.Fatal(e)
			}
			defer ic.CloseNow()
			p := <-wo
			im := openViewerMessage(t, ic)
			if failWrite {
				p.writeError = errors.New("fixture partial write")
			}
			frame := inputFrame(im.ViewerID, 1, "one-write-only")
			_ = ic.Write(context.Background(), websocket.MessageBinary, frame)
			select {
			case <-p.input:
			case <-time.After(2 * time.Second):
				t.Fatal("input missing")
			}
			want := "input_uncertain"
			if !failWrite {
				_ = ic.Write(context.Background(), websocket.MessageBinary, frame)
				want = "input_invalid"
			}
			_, data := readViewer(t, ic)
			if !strings.Contains(string(data), want) {
				t.Fatalf("unexpected close: %s", data)
			}
			waitDetached(t, p.pipeTerminal)
			select {
			case <-p.input:
				t.Fatal("input retried or replayed")
			default:
			}
		})
	}
}

func TestInteractiveFloodIsBoundedAndAudited(t *testing.T) {
	h, url, ro := terminalHarness(t)
	wo := enableInteractive(h)
	c, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	<-ro
	m := openViewerMessage(t, c)
	ic, _, e := dialViewer(t, h, url, modeTicket(t, h, m.ViewerID, "interactive", 201), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer ic.CloseNow()
	p := <-wo
	im := openViewerMessage(t, ic)
	for i := uint64(1); i <= 16; i++ {
		if e := ic.Write(context.Background(), websocket.MessageBinary, inputFrame(im.ViewerID, i, strings.Repeat("x", 16384))); e != nil {
			break
		}
	}
	waitDetached(t, p.pipeTerminal)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var count int
		if e = h.db.QueryRow("SELECT count(*) FROM audit_events WHERE action='terminal.close' AND result='input_limit'").Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("input flood was not bounded/audited")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestModeAuditFailureKeepsReadOnlyViewer(t *testing.T) {
	h, url, ro := terminalHarness(t)
	enableInteractive(h)
	c, _, e := dialViewer(t, h, url, issueTicket(t, h), testOrigin)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	p := <-ro
	m := openViewerMessage(t, c)
	if _, e = h.db.Exec(`CREATE TRIGGER reject_mode_audit BEFORE INSERT ON audit_events WHEN NEW.action='terminal.mode' BEGIN SELECT RAISE(ABORT,'fixture audit failure'); END`); e != nil {
		t.Fatal(e)
	}
	modeTicket(t, h, m.ViewerID, "interactive", 500)
	select {
	case <-p.closed:
		t.Fatal("audit failure detached viewer or activated input")
	default:
	}
	if _, e = h.db.Exec("DROP TRIGGER reject_mode_audit"); e != nil {
		t.Fatal(e)
	}
	modeTicket(t, h, m.ViewerID, "interactive", 201)
	waitDetached(t, p)
}
