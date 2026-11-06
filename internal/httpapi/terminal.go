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
	Expires                                                  time.Time
}

var sessionTarget = regexp.MustCompile(`^\$[0-9]{1,16}$`)
var terminalEpoch = regexp.MustCompile(`^e_[0-9a-f]{64}$`)

func (s *Server) terminalOptions(w http.ResponseWriter, r *http.Request) {
	s.json(w, 200, map[string]any{"enabled": s.cfg.TerminalEnabled, "mode": "read_only"})
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
	h, record, err := s.terminalAccess(r.Context(), u, body.HostID)
	if err != nil {
		s.fail(w, r, 404, "not_found", "Host SSH tidak tersedia atau tidak diizinkan.")
		return
	}
	t := terminalTicket{UserID: u.ID, TokenHash: identity.TokenHash(token(r)), HostID: h.ID, Socket: h.SocketName, KeyRef: record.KeyRef, Target: body.SessionID, Epoch: body.Epoch, Expires: time.Now().Add(30 * time.Second)}
	raw := storage.ID("") + storage.ID("")
	s.viewMu.Lock()
	count := 0
	for hash, ticket := range s.tickets {
		if time.Now().After(ticket.Expires) {
			delete(s.tickets, hash)
		} else if ticket.TokenHash == t.TokenHash {
			count++
		}
	}
	allowed := len(s.tickets) < 64 && count < 8
	if allowed {
		s.tickets[identity.TokenHash(raw)] = t
	}
	s.viewMu.Unlock()
	if !allowed {
		s.fail(w, r, 429, "busy", "Terlalu banyak permintaan viewer. Tunggu 30 detik.")
		return
	}
	s.json(w, 201, map[string]any{"ticket": raw, "expiresIn": 30})
}

func (s *Server) terminalAccess(ctx context.Context, u identity.User, id string) (Host, sshconnector.Record, error) {
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM hosts WHERE id=? AND transport='ssh' AND (? OR id IN (SELECT host_id FROM host_access WHERE user_id=? AND action='tmux:read'))`, id, u.IsAdmin, u.ID).Scan(&found)
	if err != nil {
		return Host{}, sshconnector.Record{}, err
	}
	h, err := s.observedHost(ctx, found)
	if err != nil {
		return h, sshconnector.Record{}, err
	}
	if !s.cfg.AllowedSSHHosts[h.TailnetAddress] {
		return h, sshconnector.Record{}, sql.ErrNoRows
	}
	record, err := s.credentials().Load(ctx, id)
	return h, record, err
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
			if p == "tmux.v1" {
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
	h, record, err := s.terminalAccess(r.Context(), user(r), t.HostID)
	if err != nil || record.KeyRef != t.KeyRef || h.SocketName != t.Socket {
		s.fail(w, r, 404, "not_found", "Host SSH tidak tersedia atau tidak diizinkan.")
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
		s.viewers[viewer] = t
		s.terminalWG.Add(1)
	}
	s.viewMu.Unlock()
	if !allowed {
		s.fail(w, r, 429, "busy", "Batas viewer aktif tercapai. Tutup viewer lain terlebih dahulu.")
		return
	}
	defer func() { s.viewMu.Lock(); delete(s.viewers, viewer); s.viewMu.Unlock(); s.terminalWG.Done() }()
	// Origin was compared byte-for-byte by authorize. Accept also performs its
	// own host-based check; do not use InsecureSkipVerify.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"tmux.v1"}, OriginPatterns: []string{strings.TrimPrefix(strings.TrimPrefix(s.cfg.Origin, "https://"), "http://")}, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	s.serveTerminal(r, conn, t, h, record, viewer)
}

func (s *Server) terminalAudit(r *http.Request, t terminalTicket, action, result string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(actor,action,host_id,target,result,request_id,at) VALUES (?,?,?,?,?,?,?)`, t.UserID, action, t.HostID, t.Epoch+":"+t.Target, result, requestID(r), storage.Now())
	return err
}

func (s *Server) serveTerminal(r *http.Request, conn *websocket.Conn, t terminalTicket, h Host, record sshconnector.Record, viewer string) {
	lifetime, cancelLifetime := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancelLifetime()
	ctx, cancel := context.WithCancelCause(lifetime)
	defer cancel(errors.New("detached"))
	stopShutdown := context.AfterFunc(s.terminalRoot, func() { cancel(errors.New("hub_shutdown")) })
	defer stopShutdown()
	conn.SetReadLimit(64 * 1024)
	var wg sync.WaitGroup
	defer func() { _ = conn.CloseNow(); wg.Wait() }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Read cancellation closes the WebSocket immediately. Leave this reader
		// alive until CloseNow so the writer can send a final reason first.
		kind, data, err := conn.Read(context.Background())
		if err != nil {
			cancel(errors.New("disconnected"))
			return
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
	}()
	writeJSON := func(value any) error {
		data, _ := json.Marshal(value)
		deadline, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		return conn.Write(deadline, websocket.MessageText, data)
	}
	stream, err := s.terminal.OpenTerminal(ctx, sshconnector.Host{ID: h.ID, Address: h.TailnetAddress, User: h.OSUser, Socket: h.SocketName}, record, t.Target, t.Epoch)
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
	defer stream.Close()
	if err = s.terminalAudit(r, t, "terminal.open", "success"); err != nil {
		cancel(errors.New("audit_unavailable"))
		return
	}
	cols, rows := stream.Size()
	if err = writeJSON(map[string]any{"type": "terminal.opened", "viewerId": viewer, "mode": "read_only", "cols": cols, "rows": rows, "generation": 1, "sequence": 0}); err != nil {
		cancel(errors.New("disconnected"))
	}
	defer func() {
		_ = stream.Close()
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
	queue := make(chan []byte, 32) // 32 * 32 KiB = 1 MiB; never persist output
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
				cancel(errors.New("ssh_closed"))
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
	var sequence uint64
	for {
		select {
		case <-ctx.Done():
			return
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
	done := make(chan struct{})
	go func() { s.terminalWG.Wait(); close(done) }()
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
	h, record, err := s.terminalAccess(ctx, u, t.HostID)
	if err != nil || record.KeyRef != t.KeyRef || h.SocketName != t.Socket {
		return identity.ErrCredentials
	}
	return nil
}
