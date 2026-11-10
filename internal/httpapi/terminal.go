package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

type terminalOpener interface {
	OpenTerminal(context.Context, sshconnector.Host, sshconnector.Record, string, string) (sshconnector.Terminal, error)
}
type terminalTicket struct {
	UserID, TokenHash, HostID, Socket, KeyRef, Target, Epoch string
	AgentID                                                  string
	AgentGeneration                                          int64
	Expires                                                  time.Time
	Mode                                                     string
}

var sessionTarget = regexp.MustCompile(`^\$[0-9]{1,16}$`)
var terminalEpoch = regexp.MustCompile(`^e_[0-9a-f]{64}$`)

func (s *Server) terminalOptions(w http.ResponseWriter, r *http.Request) {
	s.json(w, 200, map[string]any{"enabled": s.cfg.TerminalEnabled, "mode": "read_only",
		"paneSnapshotEnabled": s.cfg.PaneSnapshotEnabled, "agentTerminalEnabled": s.agentTerminalEnabled()})
}

func (s *Server) terminalTicket(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.TerminalEnabled {
		s.fail(w, r, 503, "terminal_disabled", "Viewer terminal belum diaktifkan oleh operator hub.")
		return
	}
	var body struct {
		HostID    string `json:"hostId"`
		SessionID string `json:"sessionId"`
		Epoch     string `json:"epoch"`
	}
	if !s.decode(w, r, &body) {
		return
	}
	if len(body.HostID) > 80 || !sessionTarget.MatchString(body.SessionID) || !terminalEpoch.MatchString(body.Epoch) {
		s.fail(w, r, 422, "invalid_request", "Target session atau epoch tidak valid.")
		return
	}
	u := user(r)
	h, record, peer, err := s.viewerAccess(r.Context(), u, body.HostID)
	if err != nil {
		s.fail(w, r, 404, "not_found", "Host terminal tidak tersedia atau tidak diizinkan.")
		return
	}
	t := terminalTicket{UserID: u.ID, TokenHash: identity.TokenHash(token(r)), HostID: h.ID, Socket: h.SocketName, KeyRef: record.KeyRef, Target: body.SessionID, Epoch: body.Epoch, Mode: "read_only"}
	if peer != nil {
		t.AgentID, t.AgentGeneration = peer.id, peer.generation
	}
	raw, allowed := s.issueTerminalTicket(t, 30*time.Second)
	if !allowed {
		s.fail(w, r, 429, "busy", "Terlalu banyak permintaan viewer. Tunggu 30 detik.")
		return
	}
	s.json(w, 201, map[string]any{"ticket": raw, "expiresIn": 30})
}

func (s *Server) terminalAccess(ctx context.Context, u identity.User, id string) (Host, sshconnector.Record, error) {
	h, err := s.viewerHost(ctx, u, id, "ssh")
	if err != nil {
		return h, sshconnector.Record{}, err
	}
	record, err := s.sshViewerCredentials(ctx, h)
	return h, record, err
}

func (s *Server) viewerHost(ctx context.Context, u identity.User, id, transport string) (Host, error) {
	return scanObservedHost(s.db.QueryRowContext(ctx, "SELECT "+observedHostColumns+observedHostJoin+
		`WHERE h.id=? AND (?='' OR h.transport=?) AND (? OR h.id IN
		(SELECT host_id FROM host_access WHERE user_id=? AND action='tmux:read'))`, id, transport, transport, u.IsAdmin, u.ID))
}

func (s *Server) sshViewerCredentials(ctx context.Context, h Host) (sshconnector.Record, error) {
	if !s.cfg.AllowedSSHHosts[h.TailnetAddress] {
		return sshconnector.Record{}, sql.ErrNoRows
	}
	return s.credentials().Load(ctx, h.ID)
}

func (s *Server) viewerAccess(ctx context.Context, u identity.User, id string) (Host, sshconnector.Record, *agentPeer, error) {
	h, err := s.viewerHost(ctx, u, id, "")
	if err != nil {
		return h, sshconnector.Record{}, nil, err
	}
	if h.Transport == "ssh" {
		record, err := s.sshViewerCredentials(ctx, h)
		return h, record, nil, err
	}
	p := s.agents.peer(id)
	if h.Transport != "agent" || !s.agentTerminalEnabled() || p == nil || !p.terminal ||
		p.socket != h.SocketName || p.osUser != h.OSUser || p.ctx.Err() != nil {
		return h, sshconnector.Record{}, nil, sql.ErrNoRows
	}
	select {
	case <-p.ready:
	default:
		return h, sshconnector.Record{}, nil, sql.ErrNoRows
	}
	var active bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_identities
		WHERE id=? AND host_id=? AND generation=? AND revoked_at IS NULL)`, p.id, h.ID, p.generation).Scan(&active)
	if err == nil && !active {
		err = sql.ErrNoRows
	}
	return h, sshconnector.Record{}, p, err
}

func (t terminalTicket) matches(h Host, record sshconnector.Record, peer *agentPeer) bool {
	if h.SocketName != t.Socket {
		return false
	}
	if peer == nil {
		return h.Transport == "ssh" && t.AgentID == "" && record.KeyRef == t.KeyRef
	}
	return h.Transport == "agent" && peer.id == t.AgentID && peer.generation == t.AgentGeneration
}

func (s *Server) terminalUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.TerminalEnabled {
		s.fail(w, r, 503, "terminal_disabled", "Viewer terminal belum diaktifkan oleh operator hub.")
		return
	}
	var raw string
	version := false
	protocols := strings.Split(strings.Join(r.Header.Values("Sec-WebSocket-Protocol"), ","), ",")
	if len(protocols) == 2 {
		for _, p := range protocols {
			p = strings.TrimSpace(p)
			if p == "tmux.v1" || p == "tmux.v2" {
				version = true
			} else if strings.HasPrefix(p, "ticket.") {
				raw = strings.TrimPrefix(p, "ticket.")
			}
		}
	}
	if !version || len(raw) != 64 || r.URL.RawQuery != "" {
		s.fail(w, r, 403, "forbidden", "Protokol atau tiket viewer tidak valid.")
		return
	}
	s.viewMu.Lock()
	t, ok := s.tickets[identity.TokenHash(raw)]
	delete(s.tickets, identity.TokenHash(raw))
	s.viewMu.Unlock()
	if !ok || time.Now().After(t.Expires) || t.UserID != user(r).ID || t.TokenHash != identity.TokenHash(token(r)) {
		s.fail(w, r, 403, "forbidden", "Tiket viewer kedaluwarsa atau sudah dipakai.")
		return
	}
	h, record, peer, err := s.viewerAccess(r.Context(), user(r), t.HostID)
	if err != nil || !t.matches(h, record, peer) {
		s.fail(w, r, 404, "not_found", "Host terminal tidak tersedia atau tidak diizinkan.")
		return
	}
	if err := s.checkViewer(r.Context(), r, t); err != nil {
		s.fail(w, r, 403, "forbidden", "Akses viewer tidak lagi berlaku.")
		return
	}
	viewer := storage.ID("")
	s.viewMu.Lock()
	users, hosts := 0, 0
	for _, v := range s.viewers {
		if v.UserID == t.UserID {
			users++
		}
		if v.HostID == t.HostID {
			hosts++
		}
	}
	allowed := !s.terminalClosing && len(s.viewers) < 8 && users < 4 && hosts < 4
	if allowed {
		s.viewers[viewer] = &terminalViewer{terminalTicket: t, Done: make(chan struct{})}
		s.terminalWG.Add(1)
	}
	s.viewMu.Unlock()
	if !allowed {
		s.fail(w, r, 429, "busy", "Batas viewer aktif tercapai. Tutup viewer lain terlebih dahulu.")
		return
	}
	s.viewMu.Lock()
	reservation := s.viewers[viewer]
	s.viewMu.Unlock()
	defer func() {
		s.viewMu.Lock()
		delete(s.viewers, viewer)
		s.viewMu.Unlock()
		close(reservation.Done)
		s.terminalWG.Done()
	}()
	// Origin was compared byte-for-byte by authorize. Accept also performs its
	// own host-based check; do not use InsecureSkipVerify.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"tmux.v2", "tmux.v1"}, OriginPatterns: []string{strings.TrimPrefix(strings.TrimPrefix(s.cfg.Origin, "https://"), "http://")}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	s.serveTerminal(r, conn, t, h, record, peer, viewer, reservation)
}

func (s *Server) terminalAudit(r *http.Request, t terminalTicket, action, result string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(actor,action,host_id,target,result,request_id,at) VALUES (?,?,?,?,?,?,?)`, t.UserID, action, t.HostID, t.Epoch+":"+t.Target, result, requestID(r), storage.Now())
	return err
}

func (s *Server) openViewer(ctx context.Context, t terminalTicket, h Host, record sshconnector.Record, peer *agentPeer) (sshconnector.Terminal, error) {
	if peer != nil {
		stream, err := peer.openTerminal(ctx, t)
		if err != nil {
			return nil, err
		}
		return stream, nil
	}
	host := sshconnector.Host{ID: h.ID, Address: h.TailnetAddress, User: h.OSUser, Socket: h.SocketName}
	if t.Mode == "interactive" {
		if interactive, ok := s.terminal.(interactiveOpener); ok {
			return interactive.OpenInteractiveTerminal(ctx, host, record, t.Target, t.Epoch)
		}
		return nil, &sshconnector.Failure{Code: "interactive_unavailable"}
	}
	return s.terminal.OpenTerminal(ctx, host, record, t.Target, t.Epoch)
}

func (s *Server) serveTerminal(r *http.Request, conn *websocket.Conn, t terminalTicket, h Host, record sshconnector.Record, peer *agentPeer, viewer string, reservation *terminalViewer) {
	lifetime, cancelLifetime := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancelLifetime()
	ctx, cancel := context.WithCancelCause(lifetime)
	defer cancel(errors.New("detached"))
	stopShutdown := context.AfterFunc(s.terminalRoot, func() { cancel(errors.New("hub_shutdown")) })
	defer stopShutdown()
	conn.SetReadLimit(64 * 1024)
	var wg sync.WaitGroup
	defer func() { _ = conn.CloseNow(); wg.Wait() }()
	s.viewMu.Lock()
	reservation.Cancel = cancel
	s.viewMu.Unlock()
	incoming := make(chan []byte, 8)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			kind, data, err := conn.Read(context.Background())
			if err != nil {
				cancel(errors.New("disconnected"))
				return
			}
			if kind == websocket.MessageBinary && t.Mode == "interactive" {
				if len(data) <= 30 || len(data) > 30+16384 {
					cancel(errors.New("input_limit"))
					return
				}
				select {
				case incoming <- data:
				case <-ctx.Done():
					return
				default:
					cancel(errors.New("input_limit"))
					return
				}
				continue
			}
			var control struct {
				Type string `json:"type"`
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if kind == websocket.MessageText && decoder.Decode(&control) == nil && control.Type == "terminal.detach" && decoder.Decode(new(any)) == io.EOF {
				cancel(errors.New("detached"))
				return
			}
			cancel(errors.New("input_forbidden"))
			return
		}
	}()
	writeJSON := func(value any) error {
		data, _ := json.Marshal(value)
		deadline, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		return conn.Write(deadline, websocket.MessageText, data)
	}
	stream, err := s.openViewer(ctx, t, h, record, peer)
	if err == nil {
		err = s.checkViewer(ctx, r, t)
	}
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		code := sshconnector.Code(err)
		if ctx.Err() != nil {
			code = context.Cause(ctx).Error()
		}
		_ = s.terminalAudit(r, t, "terminal.open", code)
		_ = writeJSON(map[string]string{"type": "error", "code": code})
		cancel(err)
		return
	}
	closeStream := sync.OnceFunc(func() {
		err := stream.Close()
		if peer != nil {
			reservation.AgentDetached = err == nil
		}
	})
	defer closeStream()
	result := "success"
	if t.Mode == "interactive" {
		result = "success_interactive"
	}
	if err = s.terminalAudit(r, t, "terminal.open", result); err != nil {
		cancel(errors.New("audit_unavailable"))
		return
	}
	cols, rows := stream.Size()
	if err = writeJSON(map[string]any{"type": "terminal.opened", "viewerId": viewer, "mode": t.Mode, "canWrite": s.canWriteTerminal(ctx, r, t), "cols": cols, "rows": rows, "generation": 1, "sequence": 0}); err != nil {
		cancel(errors.New("disconnected"))
	}
	defer func() {
		closeStream()
		reason := "disconnected"
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		messageType := "terminal.closed"
		if reason == "slow_consumer" {
			messageType = "resync_required"
		}
		_ = writeJSON(map[string]string{"type": messageType, "reason": reason})
		if err := s.terminalAudit(r, t, "terminal.close", reason); err != nil {
			s.log.Error("terminal audit failed", "request_id", requestID(r))
		}
	}()
	queueSize := 32
	if peer != nil {
		queueSize -= agentOutputQueue
	}
	queue := make(chan []byte, queueSize) // gateway + Agent relay queue total at most 1 MiB
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			data := make([]byte, 32*1024)
			n, e := stream.Read(data)
			if n > 0 {
				select {
				case queue <- data[:n]:
				default:
					cancel(errors.New("slow_consumer"))
					return
				}
			}
			if e != nil {
				if peer != nil {
					cancel(errors.New(sshconnector.Code(e)))
				} else {
					cancel(errors.New("ssh_closed"))
				}
				return
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		tick := time.NewTicker(s.terminalInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := s.checkViewer(ctx, r, t); err != nil {
					cancel(errors.New("access_revoked"))
					return
				}
				if err := stream.Check(ctx); err != nil {
					cancel(errors.New(sshconnector.Code(err)))
					return
				}
			}
		}
	}()
	id, _ := hex.DecodeString(viewer)
	heartbeats := make(chan struct{}, 1)
	if conn.Subprotocol() == "tmux.v2" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick := time.NewTicker(s.terminalPingInterval)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					pingCtx, done := context.WithTimeout(ctx, s.terminalPingTimeout)
					err := conn.Ping(pingCtx)
					done()
					if err != nil {
						cancel(errors.New("heartbeat_timeout"))
						return
					}
					select {
					case heartbeats <- struct{}{}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	var sequence, inputSequence uint64
	budget := float64(32 * 1024)
	lastInput := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeats:
			if err := writeJSON(map[string]any{"type": "terminal.heartbeat", "viewerId": viewer, "generation": 1}); err != nil {
				cancel(errors.New("disconnected"))
				return
			}
		case frame := <-incoming:
			if len(frame) <= 30 || frame[0] != 1 || frame[1] != 1 || binary.BigEndian.Uint32(frame[2:6]) != 1 || binary.BigEndian.Uint64(frame[6:14]) != inputSequence+1 || !bytes.Equal(frame[14:30], id) {
				cancel(errors.New("input_invalid"))
				return
			}
			now := time.Now()
			budget = min(float64(32*1024), budget+now.Sub(lastInput).Seconds()*64*1024)
			lastInput = now
			if float64(len(frame)-30) > budget {
				cancel(errors.New("input_limit"))
				return
			}
			budget -= float64(len(frame) - 30)
			if err := s.checkViewer(ctx, r, t); err != nil {
				cancel(errors.New("access_revoked"))
				return
			}
			input, ok := stream.(sshconnector.InputTerminal)
			if !ok {
				cancel(errors.New("input_forbidden"))
				return
			}
			inputSequence++
			writeCtx, done := context.WithTimeout(ctx, 2*time.Second)
			err := input.WriteInput(writeCtx, frame[30:])
			done()
			if err != nil {
				cancel(errors.New("input_uncertain"))
				return
			}
		case data := <-queue:
			sequence++
			frame := make([]byte, 30+len(data))
			frame[0] = 1
			frame[1] = 0
			binary.BigEndian.PutUint32(frame[2:6], 1)
			binary.BigEndian.PutUint64(frame[6:14], sequence)
			copy(frame[14:30], id)
			copy(frame[30:], data)
			deadline, done := context.WithTimeout(ctx, 2*time.Second)
			err = conn.Write(deadline, websocket.MessageBinary, frame)
			done()
			if err != nil {
				cancel(errors.New("slow_consumer"))
				return
			}
		}
	}
}

// Shutdown explicitly closes hijacked WebSockets; net/http.Shutdown does not.
func (s *Server) Shutdown(ctx context.Context) error {
	s.viewMu.Lock()
	s.terminalClosing = true
	s.stopTerminals()
	s.viewMu.Unlock()
	s.agents.stop()
	done := make(chan struct{})
	go func() {
		s.terminalWG.Wait()
		s.agents.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) checkViewer(ctx context.Context, r *http.Request, t terminalTicket) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	u, err := identity.Authenticate(ctx, s.db, token(r))
	if err != nil || u.ID != t.UserID {
		return identity.ErrCredentials
	}
	h, record, peer, err := s.viewerAccess(ctx, u, t.HostID)
	if err != nil || !t.matches(h, record, peer) {
		return identity.ErrCredentials
	}
	if t.Mode == "interactive" && !s.canWriteTerminal(ctx, r, t) {
		return identity.ErrCredentials
	}
	return nil
}
