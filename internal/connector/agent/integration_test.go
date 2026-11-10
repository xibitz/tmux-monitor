package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/config"
	"github.com/xibitz/tmux-monitor/internal/httpapi"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

const labHost = "hub.integration.ts.net"
const labOrigin = "https://" + labHost
const labIP = "100.64.3.2"

type fixtureTransport struct {
	target *url.URL
	next   http.RoundTripper
}

func (f fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// All fixture traffic, including real TLS and WSS, goes to one localhost
	// server. The synthetic Serve headers are inserted only by that fixture.
	copy := req.Clone(req.Context())
	copy.URL.Scheme, copy.URL.Host, copy.Host = f.target.Scheme, f.target.Host, labHost
	return f.next.RoundTrip(copy)
}

func TestAgentEndToEndWithIsolatedTmux(t *testing.T) {
	version, err := exec.Command("tmux", "-V").Output()
	if err != nil || strings.TrimSpace(string(version)) != "tmux 3.8" {
		t.Skip("qualified tmux 3.8 required")
	}
	dir, err := os.MkdirTemp("/tmp", "mtm-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	socket := "agent-lab"
	command := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "tmux", append([]string{"-L", socket, "-f", "/dev/null"}, args...)...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture tmux %s: %v %s", args[0], err, data)
		}
		return strings.TrimSpace(string(data))
	}
	t.Cleanup(func() {
		cmd := exec.Command("tmux", "-L", socket, "kill-server")
		cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+dir, "TMUX=")
		_ = cmd.Run()
		_ = os.RemoveAll(dir)
	})
	command("new-session", "-d", "-s", "agent-lab", "-x", "100", "-y", "32", "printf 'fixture-ready\n'; while IFS= read -r line; do printf '<%s>\n' \"$line\"; done")
	command("set-option", "-gw", "window-size", "manual")
	command("set-option", "-w", "-t", "agent-lab", "window-size", "manual")
	command("resize-window", "-t", "agent-lab", "-x", "100", "-y", "31")
	dimensions := command("display-message", "-p", "-t", "agent-lab", "#{window_width}x#{window_height}")

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	dataDir := t.TempDir()
	db, err := storage.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const password = "fixture-password-only"
	if err = storage.Migrate(ctx, db, storage.LatestVersion); err != nil {
		t.Fatal(err)
	}
	if err = identity.Bootstrap(ctx, db, "admin", password); err != nil {
		t.Fatal(err)
	}
	const owner = "fixture"
	hostID := storage.ID("h_")
	if _, err = db.Exec(`INSERT INTO hosts(id,label,transport,os_user,socket_name,created_at,updated_at)
		VALUES (?, 'Agent fixture', 'agent', ?, ?, ?, ?)`, hostID, owner, socket, storage.Now(), storage.Now()); err != nil {
		t.Fatal(err)
	}
	hub := httpapi.New(db, config.Config{Origin: labOrigin, Listen: "127.0.0.1:8080", DataDir: dataDir,
		AgentEnabled: true, AgentHeartbeatSeconds: 5, TerminalEnabled: true, AgentTerminalEnabled: true,
		ControlEvents: true, CodingHooks: true}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ws/v1/agent") || r.URL.Path == "/api/v1/agents/enroll" {
			r.Header.Set("X-Forwarded-For", labIP)
			r.Header.Set("X-Forwarded-Host", labHost)
			r.Header.Set("X-Forwarded-Proto", "https")
		}
		hub.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := hub.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
	}()
	target, _ := url.Parse(server.URL)
	client := server.Client()
	client.Transport = fixtureTransport{target: target, next: client.Transport}

	var cookie *http.Cookie
	var csrf string
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequestWithContext(ctx, method, labOrigin+path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", labOrigin)
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		hub.ServeHTTP(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder, status int, result any) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body)
		}
		if result != nil {
			if err := json.Unmarshal(w.Body.Bytes(), result); err != nil {
				t.Fatal(err)
			}
		}
	}
	login := request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": password})
	var loginBody struct{ CSRFToken string }
	decode(login, 200, &loginBody)
	cookie, csrf = login.Result().Cookies()[0], loginBody.CSRFToken
	var enrollment struct{ Token string }
	decode(request("POST", "/api/v1/agents/enrollments", map[string]string{"hostId": hostID, "tailnetIp": labIP}), 201, &enrollment)
	pair := func() (int, agentproto.EnrollResult) {
		t.Helper()
		data, _ := json.Marshal(agentproto.EnrollRequest{HostID: hostID, OSUser: owner, Socket: socket})
		req, err := http.NewRequestWithContext(ctx, "POST", labOrigin+"/api/v1/agents/enroll", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+enrollment.Token)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result agentproto.EnrollResult
		if response.StatusCode == 201 && json.NewDecoder(response.Body).Decode(&result) != nil {
			t.Fatal("bad enrollment response")
		}
		return response.StatusCode, result
	}
	status, pairing := pair()
	if status != 201 || !agentproto.ValidAgentID(pairing.AgentID) || !agentproto.ValidSecret(pairing.Credential) {
		t.Fatalf("pairing failed: HTTP %d", status)
	}
	if status, _ := pair(); status != 401 {
		t.Fatalf("pairing token replay: HTTP %d", status)
	}

	b := Binding{Hub: labOrigin, HostID: hostID, AgentID: pairing.AgentID, OSUser: owner, Socket: socket, Credential: pairing.Credential}
	endpoint := "wss://" + labHost + "/ws/v1/agent"
	options := RunOptions{Terminal: true, ControlEvents: true, HookSocket: dir + "/hooks.sock"}
	var generation int64
	instance := storage.ID("")
	startAgent := func() (context.CancelFunc, <-chan error) {
		lifetime, stop := context.WithCancel(ctx)
		finished := make(chan error, 1)
		go func() {
			_, err := b.connect(lifetime, client, endpoint, runtime.GOOS, instance, &generation, options)
			finished <- err
		}()
		return stop, finished
	}
	wait := func(condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			if condition() {
				return
			}
			time.Sleep(60 * time.Millisecond)
		}
		t.Fatal("fixture state did not converge")
	}
	inventory := func() (tmux.Inventory, bool) {
		t.Helper()
		result := request("GET", "/api/v1/hosts/"+hostID+"/inventory", nil)
		if result.Code != 200 {
			return tmux.Inventory{}, false
		}
		var inv tmux.Inventory
		decode(result, 200, &inv)
		return inv, !inv.Stale && len(inv.Sessions) == 1 && inv.Capabilities.PTYAttach
	}
	stopAgent, finished := startAgent()
	defer stopAgent()
	var inv tmux.Inventory
	wait(func() bool {
		var ok bool
		inv, ok = inventory()
		return ok && inv.Capabilities.ControlMode
	})
	wait(func() bool {
		return strings.Contains(request("GET", "/api/v1/hosts", nil).Body.String(), "agentLastHeartbeat")
	})
	firstGeneration := inv.AgentGeneration
	var otherGeneration int64
	if _, err := b.connect(ctx, client, endpoint, runtime.GOOS, storage.ID(""), &otherGeneration, RunOptions{}); !errors.Is(err, errAgentRejected) {
		t.Fatalf("concurrent Agent instance was not rejected: %v", err)
	}
	dial := func(path string, protocols ...string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "wss://"+labHost+path, &websocket.DialOptions{
			HTTPClient: client, Subprotocols: protocols, HTTPHeader: http.Header{"Origin": {labOrigin}, "Cookie": {cookie.String()}},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		return conn
	}
	wait(func() bool { return strings.Contains(command("list-clients", "-F", "#{client_flags}"), "no-output") })
	// Allow the watcher's initial notification to precede the browser stream.
	time.Sleep(1100 * time.Millisecond)
	events := dial("/ws/v1/events?hostId=" + hostID)
	read := func(conn *websocket.Conn) (websocket.MessageType, []byte) {
		t.Helper()
		readCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		defer stop()
		kind, data, err := conn.Read(readCtx)
		if err != nil {
			t.Fatal(err)
		}
		return kind, data
	}
	_, event := read(events)
	if !bytes.Contains(event, []byte("resync_required")) {
		t.Fatalf("no initial resync: %s", event)
	}
	_, event = read(events)
	if !bytes.Contains(event, []byte("resync_required")) {
		t.Fatalf("no initial Agent generation resync: %s", event)
	}
	command("rename-window", "-t", "agent-lab", "changed-externally")
	_, event = read(events)
	if !bytes.Contains(event, []byte("inventory.changed")) {
		t.Fatalf("invalid watcher signal: %s", event)
	}
	wait(func() bool {
		latest, ok := inventory()
		return ok && latest.Sessions[0].Windows[0].Name == "changed-externally"
	})
	if got := command("display-message", "-p", "-t", "agent-lab", "#{window_width}x#{window_height}"); got != dimensions {
		t.Fatalf("observer resized fixture: %s → %s", dimensions, got)
	}

	pane := inv.Sessions[0].Windows[0].Panes[0].ID
	t.Setenv("TMUX_PANE", pane)
	t.Setenv("TMUX", command("display-message", "-p", "-t", pane, "#{socket_path},#{pid},0"))
	wait(func() bool { _, err := os.Stat(options.HookSocket); return err == nil })
	hook := []string{"--socket", options.HookSocket, "--source", "codex",
		`{"type":"agent-turn-complete","thread-id":"fixture-thread","turn-id":"fixture-turn","last-assistant-message":"private-fixture-marker"}`}
	if err := NotifyHook(ctx, hook, nil); err != nil {
		t.Fatal(err)
	}
	notices := func() string { return request("GET", "/api/v1/notifications", nil).Body.String() }
	wait(func() bool { return strings.Contains(notices(), "verified_pane") })
	time.Sleep(600 * time.Millisecond)
	if err := NotifyHook(ctx, hook, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	t.Setenv("TMUX", "")
	claude := []string{"--socket", options.HookSocket, "--source", "claude"}
	if err := NotifyHook(ctx, claude, strings.NewReader(`{"hook_event_name":"StopFailure","error":"private-fixture-marker"}`)); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return strings.Contains(notices(), `"kind":"error"`) })
	inbox := notices()
	if strings.Count(inbox, `"source":"codex"`) != 1 || !strings.Contains(inbox, `"confidence":"unknown"`) || strings.Contains(inbox, "private-fixture-marker") {
		t.Fatalf("unexpected fixture inbox: %s", inbox)
	}

	var ticket struct{ Ticket string }
	decode(request("POST", "/api/v1/terminal/tickets", map[string]string{
		"hostId": hostID, "sessionId": inv.Sessions[0].ID, "epoch": inv.Epoch,
	}), 201, &ticket)
	viewer := dial("/ws/v1/terminal", "tmux.v2", "ticket."+ticket.Ticket)
	var opened struct{ Type, Mode, ViewerID string }
	_, data := read(viewer)
	if json.Unmarshal(data, &opened) != nil || opened.Type != "terminal.opened" || opened.Mode != "read_only" {
		t.Fatalf("Agent read-only viewer failed: %s", data)
	}
	decode(request("POST", "/api/v1/terminal/viewers/"+opened.ViewerID+"/mode-tickets", map[string]string{"mode": "interactive"}), 201, &ticket)
	interactive := dial("/ws/v1/terminal", "tmux.v2", "ticket."+ticket.Ticket)
	_, data = read(interactive)
	if json.Unmarshal(data, &opened) != nil || opened.Type != "terminal.opened" || opened.Mode != "interactive" {
		t.Fatalf("Agent interactive viewer failed: %s", data)
	}
	viewerID, err := hex.DecodeString(opened.ViewerID)
	if err != nil || len(viewerID) != 16 {
		t.Fatal("invalid viewer ID")
	}
	input := make([]byte, 30)
	input[0], input[1] = 1, 1
	binary.BigEndian.PutUint32(input[2:6], 1)
	binary.BigEndian.PutUint64(input[6:14], 1)
	copy(input[14:], viewerID)
	if err := interactive.Write(ctx, websocket.MessageBinary, append(input, []byte("fixture-typed\r")...)); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return strings.Contains(command("capture-pane", "-p", "-t", pane), "<fixture-typed>") })
	stopAgent()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	wait(func() bool { latest, ok := inventory(); return !ok && latest.Stale })
	if got := command("display-message", "-p", "-t", "agent-lab", "#{window_width}x#{window_height}"); got != dimensions {
		t.Fatalf("Agent disconnect changed tmux dimensions: %s", got)
	}
	// Authenticate a process on the same instance fence and answer snapshot
	// requests, but send no heartbeats. The hub lease must close the socket.
	silent, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: client, Subprotocols: []string{agentproto.Subprotocol},
		HTTPHeader: http.Header{"Authorization": {"Bearer " + b.Credential}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer silent.CloseNow()
	if err := agentproto.Write(ctx, silent, agentproto.Hello{
		Type: "hello", Version: agentproto.Version, AgentID: b.AgentID, OS: runtime.GOOS,
		OSUser: b.OSUser, Socket: b.Socket, Tmux: "3.8", Inventory: true,
		InstanceID: instance, PreviousGeneration: generation,
	}); err != nil {
		t.Fatal(err)
	}
	var silentAck agentproto.HelloAck
	if err := agentproto.Read(ctx, silent, &silentAck); err != nil || silentAck.Generation <= generation {
		t.Fatalf("silent Agent did not connect on new generation: %v", err)
	}
	generation = silentAck.Generation
	leaseStarted := time.Now()
	leaseCtx, cancelLease := context.WithTimeout(ctx, 19*time.Second)
	defer cancelLease()
	for {
		var request agentproto.Request
		if err := agentproto.Read(leaseCtx, silent, &request); err != nil {
			if leaseCtx.Err() != nil || time.Since(leaseStarted) < 13*time.Second {
				t.Fatalf("silent Agent did not close on the heartbeat lease: %v", err)
			}
			break
		}
		if request.Type != agentproto.Snapshot {
			t.Fatalf("unexpected RPC for silent Agent: %s", request.Type)
		}
		response := snapshot(leaseCtx, socket)
		response.Type, response.RequestID, response.AgentID, response.Generation =
			agentproto.SnapshotReply, request.RequestID, b.AgentID, generation
		if err := agentproto.Write(leaseCtx, silent, response); err != nil {
			t.Fatal(err)
		}
	}
	wait(func() bool { latest, ok := inventory(); return !ok && latest.Stale })

	stopAgain, finishedAgain := startAgent()
	defer stopAgain()
	wait(func() bool {
		latest, ok := inventory()
		return ok && latest.AgentGeneration > firstGeneration
	})
	decode(request("POST", "/api/v1/agents/"+b.AgentID+"/revoke", nil), 204, nil)
	select {
	case <-finishedAgain:
	case <-ctx.Done():
		t.Fatal("Agent not disconnected on revoke")
	}
	if _, err := b.connect(ctx, client, endpoint, runtime.GOOS, instance, &generation, options); !errors.Is(err, errAgentRejected) {
		t.Fatalf("revoked Agent connected: %v", err)
	}
}
