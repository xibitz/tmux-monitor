package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type agentConnections struct {
	mu          sync.Mutex
	peers       map[string]*agentPeer
	handshakes  chan struct{}
	snapshots   chan struct{}
	closing     bool
	wg          sync.WaitGroup
	pairingFrom time.Time
	pairingHits int
}

type agentPeer struct {
	id, hostID, socket, osUser string
	generation                 int64
	conn                       *websocket.Conn
	ctx                        context.Context
	cancel                     context.CancelFunc
	ready                      chan struct{}
	terminal                   bool

	mu      sync.Mutex
	pending *agentPending
	offers  map[string]*agentStreamOffer
}

type agentPending struct {
	id       string
	reply    chan agentproto.SnapshotResult
	received bool
}

func (a *agentConnections) allowEnrollment(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Sub(a.pairingFrom) >= time.Minute {
		a.pairingFrom, a.pairingHits = now, 0
	}
	if a.pairingHits >= 40 {
		return false
	}
	a.pairingHits++
	return true
}

func (a *agentConnections) beginHandshake() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return false
	}
	select {
	case a.handshakes <- struct{}{}:
		a.wg.Add(1)
		return true
	default:
		return false
	}
}

func (a *agentConnections) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closing = true
	for _, peer := range a.peers {
		peer.cancel()
	}
}

func (a *agentConnections) disconnect(hostID, agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.peers[hostID]; p != nil && (agentID == "" || p.id == agentID) {
		delete(a.peers, hostID)
		p.cancel()
	}
}

func (a *agentConnections) register(ctx context.Context, db *sql.DB, p *agentPeer, hash, ip string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.peers[p.hostID]
	if a.closing || old == nil && len(a.peers) >= 32 {
		return errors.New("Agent connection limit")
	}
	err := db.QueryRowContext(ctx, `UPDATE agent_identities SET generation=generation+1
		WHERE id=? AND host_id=? AND credential_hash=? AND tailnet_ip=? AND revoked_at IS NULL
		RETURNING generation`, p.id, p.hostID, hash, ip).Scan(&p.generation)
	if err != nil {
		return err
	}
	a.peers[p.hostID] = p
	if old != nil {
		old.cancel()
	}
	return nil
}

func (a *agentConnections) remove(p *agentPeer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.peers[p.hostID] == p {
		delete(a.peers, p.hostID)
	}
}

func (s *Server) agentUpgrade(w http.ResponseWriter, r *http.Request) {
	hash, ip, ok := s.agentBearer(w, r)
	if !ok {
		return
	}
	if !s.agents.beginHandshake() {
		s.fail(w, r, 429, "busy", "Hub sedang menangani koneksi Agent lain.")
		return
	}
	defer s.agents.wg.Done()
	handshaking := true
	defer func() {
		if handshaking {
			<-s.agents.handshakes
		}
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stopShutdown := context.AfterFunc(s.terminalRoot, cancel)
	defer stopShutdown()
	helloCtx, stopHello := context.WithTimeout(ctx, agentproto.HandshakeTimeout)
	defer stopHello()
	p := &agentPeer{ctx: ctx, cancel: cancel, ready: make(chan struct{})}
	err := s.db.QueryRowContext(helloCtx, `SELECT a.id,h.id,h.os_user,h.socket_name
		FROM agent_identities a JOIN hosts h ON h.id=a.host_id
		WHERE a.credential_hash=? AND a.tailnet_ip=? AND a.revoked_at IS NULL AND h.transport='agent'`,
		hash, ip).Scan(&p.id, &p.hostID, &p.osUser, &p.socket)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 401, "unauthenticated", "Credential Agent tidak valid.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{agentproto.Subprotocol}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != agentproto.Subprotocol {
		return
	}
	p.conn = conn
	conn.SetReadLimit(agentproto.ControlLimit)
	var hello agentproto.Hello
	if agentproto.Read(helloCtx, conn, &hello) != nil || hello.Type != "hello" || hello.Version != agentproto.Version ||
		hello.AgentID != p.id || hello.OSUser != p.osUser || hello.Socket != p.socket ||
		(hello.OS != "linux" && hello.OS != "darwin") || !hello.Inventory ||
		(hello.Tmux != "" && !agentproto.ValidTmuxVersion(hello.Tmux)) {
		return
	}
	p.terminal = hello.Terminal
	if s.agents.register(helloCtx, s.db, p, hash, ip) != nil {
		return
	}
	defer s.agents.remove(p)
	conn.SetReadLimit(agentproto.SnapshotFrameLimit)
	ack := agentproto.HelloAck{Type: "hello.ack", Version: agentproto.Version, AgentID: p.id, Generation: p.generation}
	if agentproto.Write(helloCtx, conn, ack) != nil {
		return
	}
	stopHello()
	close(p.ready)
	<-s.agents.handshakes
	handshaking = false
	p.readResults()
}

func (p *agentPeer) readResults() {
	for {
		var result agentproto.SnapshotResult
		if agentproto.Read(p.ctx, p.conn, &result) != nil || result.Type != agentproto.SnapshotReply ||
			result.AgentID != p.id || result.Generation != p.generation || len(result.Output) > tmux.MaxOutput {
			return
		}
		p.mu.Lock()
		pending := p.pending
		if pending == nil || pending.id != result.RequestID || pending.received {
			p.mu.Unlock()
			return
		}
		pending.received = true
		pending.reply <- result
		p.mu.Unlock()
	}
}

func (a *agentConnections) peer(hostID string) *agentPeer {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.peers[hostID]
}

func (a *agentConnections) snapshot(ctx context.Context, h Host) (tmux.Inventory, *agentPeer, error) {
	p := a.peer(h.ID)
	fail := func(code string) (tmux.Inventory, *agentPeer, error) {
		return tmux.Inventory{}, p, &sshconnector.Failure{Code: code}
	}
	if p == nil || p.socket != h.SocketName || p.osUser != h.OSUser || p.ctx.Err() != nil {
		return fail("agent_offline")
	}
	select {
	case <-p.ready:
	default:
		return fail("agent_offline")
	}
	select {
	case a.snapshots <- struct{}{}:
		defer func() { <-a.snapshots }()
	default:
		return fail("busy")
	}
	if ctx.Err() != nil {
		return fail("timeout")
	}
	// A caller leaving does not let its reply leak into the next observation.
	// The connection and slot stay bounded by an independent RPC deadline.
	rpc, cancel := context.WithTimeout(p.ctx, agentproto.SnapshotTimeout)
	defer cancel()
	deadline, _ := rpc.Deadline()
	pending := &agentPending{id: storage.ID("rpc_"), reply: make(chan agentproto.SnapshotResult, 1)}
	p.mu.Lock()
	if p.pending != nil {
		p.mu.Unlock()
		return fail("busy")
	}
	p.pending = pending
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.pending == pending {
			p.pending = nil
		}
		p.mu.Unlock()
	}()
	req := agentproto.Request{Type: agentproto.Snapshot, RequestID: pending.id, AgentID: p.id, Generation: p.generation, Deadline: deadline}
	if agentproto.Write(rpc, p.conn, req) != nil {
		p.cancel()
		return fail("agent_offline")
	}
	var result agentproto.SnapshotResult
	select {
	case result = <-pending.reply:
	case <-rpc.Done():
		if p.ctx.Err() != nil {
			return fail("agent_offline")
		}
		p.cancel()
		return fail("timeout")
	}
	var inv tmux.Inventory
	switch result.Status {
	case "ok":
		if !agentproto.ValidTmuxVersion(result.Version) {
			return fail("inventory_invalid")
		}
		var err error
		inv, err = tmux.Parse(h.ID, h.SocketName, result.Version, result.Output)
		if errors.Is(err, tmux.ErrChanged) {
			return fail("inventory_changed")
		}
		if err != nil {
			return fail("inventory_invalid")
		}
	case "no_tmux_server":
		if len(result.Output) != 0 || !agentproto.ValidTmuxVersion(result.Version) {
			return fail("inventory_invalid")
		}
		inv = tmux.Empty(h.ID, h.SocketName, result.Version)
	case "tmux_missing", "inventory_invalid", "command_failed", "timeout", "output_limit":
		return fail(result.Status)
	default:
		return fail("inventory_invalid")
	}
	inv.Transport = "agent"
	return inv, p, nil
}
