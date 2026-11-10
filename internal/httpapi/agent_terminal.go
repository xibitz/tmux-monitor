package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

const agentOutputQueue = 8

type agentStreamOffer struct {
	setup context.Context
	conn  chan *websocket.Conn
	done  chan struct{}
}

func (s *Server) agentTerminalEnabled() bool {
	return s.cfg.TerminalEnabled && s.cfg.AgentTerminalEnabled && s.agentEnabled()
}

func (s *Server) agentTerminalUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.agentTerminalEnabled() {
		s.fail(w, r, 503, "terminal_disabled", "Terminal Agent belum diaktifkan.")
		return
	}
	hash, ip, ok := s.agentBearer(w, r)
	if !ok {
		return
	}
	tickets := r.Header.Values(agentproto.TerminalTicketHeader)
	if len(tickets) != 1 || !agentproto.ValidSecret(tickets[0]) {
		s.fail(w, r, 403, "forbidden", "Tiket stream Agent tidak valid.")
		return
	}
	var id, hostID string
	var generation int64
	err := s.db.QueryRowContext(r.Context(), `SELECT id,host_id,generation FROM agent_identities
		WHERE credential_hash=? AND tailnet_ip=? AND revoked_at IS NULL`,
		hash, ip).Scan(&id, &hostID, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 401, "unauthenticated", "Credential Agent tidak valid.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	p := s.agents.peer(hostID)
	if p == nil || p.id != id || p.generation != generation || !p.terminalReady("") {
		s.fail(w, r, 403, "forbidden", "Koneksi Agent tidak lagi berlaku.")
		return
	}
	key := identity.TokenHash(tickets[0])
	p.mu.Lock()
	offer := p.offers[key]
	delete(p.offers, key)
	p.mu.Unlock()
	if offer == nil || offer.setup.Err() != nil {
		s.fail(w, r, 403, "forbidden", "Tiket stream Agent kedaluwarsa atau sudah dipakai.")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{agentproto.TerminalProtocol}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != agentproto.TerminalProtocol {
		return
	}
	select {
	case offer.conn <- conn:
		select {
		case <-offer.done:
		case <-p.ctx.Done():
		}
	case <-offer.setup.Done():
	case <-p.ctx.Done():
	}
}

func (p *agentPeer) openTerminal(ctx context.Context, t terminalTicket) (*agentTerminalStream, error) {
	if !p.terminalReady(t.Epoch) {
		return nil, &sshconnector.Failure{Code: "agent_closed"}
	}
	setup, cancel := context.WithTimeout(ctx, agentproto.TerminalSetupTimeout)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	raw := storage.ID("") + storage.ID("")
	key := identity.TokenHash(raw)
	offer := &agentStreamOffer{setup: setup, conn: make(chan *websocket.Conn), done: make(chan struct{})}
	p.mu.Lock()
	if p.offers == nil {
		p.offers = make(map[string]*agentStreamOffer)
	}
	p.offers[key] = offer
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.offers, key)
		p.mu.Unlock()
	}()
	opened := false
	defer func() {
		if !opened {
			close(offer.done)
		}
	}()
	deadline, _ := setup.Deadline()
	req := agentproto.TerminalOffer{Type: agentproto.TerminalOpen, AgentID: p.id, Generation: p.generation,
		Ticket: raw, Target: t.Target, Epoch: t.Epoch, Mode: t.Mode, Deadline: deadline}
	write, done := context.WithTimeout(setup, agentproto.TerminalIOTimeout)
	err := agentproto.Write(write, p.conn, req)
	done()
	if err != nil {
		return nil, &sshconnector.Failure{Code: "agent_closed"}
	}
	var conn *websocket.Conn
	select {
	case conn = <-offer.conn:
	case <-setup.Done():
		return nil, &sshconnector.Failure{Code: "timeout"}
	}
	conn.SetReadLimit(agentproto.TerminalHeader + agentproto.MaxTerminalOutput)
	var ready agentproto.TerminalEvent
	if agentproto.Read(setup, conn, &ready) != nil || ready.Type != agentproto.TerminalReady {
		return nil, &sshconnector.Failure{Code: "terminal_attach_failed"}
	}
	if ready.Status != "ok" {
		code := ready.Status
		if !agentproto.ValidTerminalStatus(code) {
			code = "terminal_attach_failed"
		}
		return nil, &sshconnector.Failure{Code: code}
	}
	if ready.Cols < 2 || ready.Cols > 500 || ready.Rows < 2 || ready.Rows > 205 {
		return nil, &sshconnector.Failure{Code: "terminal_size_policy"}
	}
	stream := &agentTerminalStream{peer: p, conn: conn, done: offer.done, interactive: t.Mode == "interactive",
		cols: ready.Cols, rows: ready.Rows, output: make(chan []byte, agentOutputQueue), readerDone: make(chan struct{}), acks: make(chan uint64, 1)}
	opened = true
	go stream.readFrames()
	return stream, nil
}

type agentTerminalStream struct {
	peer           *agentPeer
	conn           *websocket.Conn
	done           chan struct{}
	interactive    bool
	cols, rows     int
	output         chan []byte
	readerDone     chan struct{}
	acks           chan uint64
	remaining      []byte // used only by Read
	reason         string // published by closing readerDone
	remoteDetached bool   // published by closing readerDone
	inputSequence  uint64 // one writer in the browser gateway
	expectedAck    atomic.Uint64
	once           sync.Once
	closeErr       error
}

func (t *agentTerminalStream) readFrames() {
	defer close(t.readerDone)
	t.reason = "agent_closed"
	var sequence uint64
	for {
		kind, data, err := t.conn.Read(t.peer.ctx)
		if err != nil {
			return
		}
		if kind == websocket.MessageText {
			var event agentproto.TerminalEvent
			if agentproto.Decode(data, &event) == nil && event.Type == agentproto.TerminalClosed &&
				agentproto.ValidTerminalStatus(event.Status) && event.Cols == 0 && event.Rows == 0 {
				t.reason, t.remoteDetached = event.Status, true
			}
			return
		}
		direction, next, payload, ok := agentproto.ParseTerminalFrame(data)
		if kind != websocket.MessageBinary || !ok {
			return
		}
		switch direction {
		case agentproto.TerminalOutput:
			if next != sequence+1 {
				return
			}
			sequence = next
			select {
			case t.output <- payload:
			default:
				t.reason = "slow_consumer"
				return
			}
		case agentproto.TerminalInputAck:
			if !t.interactive || !t.expectedAck.CompareAndSwap(next, 0) {
				return
			}
			select {
			case t.acks <- next:
			default:
				return
			}
		default:
			return
		}
	}
}

func (t *agentTerminalStream) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if len(t.remaining) == 0 {
		select {
		case <-t.readerDone:
			return 0, &sshconnector.Failure{Code: t.reason}
		default:
		}
		select {
		case t.remaining = <-t.output:
		case <-t.readerDone:
			return 0, &sshconnector.Failure{Code: t.reason}
		}
	}
	n := copy(dst, t.remaining)
	t.remaining = t.remaining[n:]
	return n, nil
}

func (t *agentTerminalStream) WriteInput(ctx context.Context, data []byte) error {
	if !t.interactive || len(data) < 1 || len(data) > agentproto.MaxTerminalInput {
		return &sshconnector.Failure{Code: "input_forbidden"}
	}
	select {
	case <-t.readerDone:
		return &sshconnector.Failure{Code: t.reason}
	default:
	}
	t.inputSequence++
	t.expectedAck.Store(t.inputSequence)
	defer t.expectedAck.Store(0)
	frame := agentproto.TerminalFrame(agentproto.TerminalInput, t.inputSequence, data)
	if err := t.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		return err
	}
	select {
	case ack := <-t.acks:
		if ack == t.inputSequence {
			return nil
		}
	case <-ctx.Done():
	case <-t.readerDone:
	}
	return &sshconnector.Failure{Code: "input_uncertain"}
}

func (t *agentTerminalStream) Size() (int, int) { return t.cols, t.rows }

func (t *agentTerminalStream) Check(ctx context.Context) error {
	select {
	case <-t.readerDone:
		return &sshconnector.Failure{Code: t.reason}
	default:
	}
	ping, cancel := context.WithTimeout(ctx, agentproto.TerminalIOTimeout)
	defer cancel()
	if t.conn.Ping(ping) != nil {
		return &sshconnector.Failure{Code: "agent_closed"}
	}
	return nil
}

func (t *agentTerminalStream) Close() error {
	t.once.Do(func() {
		defer close(t.done)
		defer t.conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), agentproto.TerminalDetachTimeout)
		defer cancel()
		select {
		case <-t.readerDone:
		default:
			_ = agentproto.Write(ctx, t.conn, agentproto.TerminalEvent{Type: agentproto.TerminalDetach})
		}
		select {
		case <-t.readerDone:
			if t.remoteDetached {
				return
			}
		case <-ctx.Done():
		}
		t.closeErr = errors.New("Agent terminal detach unconfirmed")
	})
	return t.closeErr
}
