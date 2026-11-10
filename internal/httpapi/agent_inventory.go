package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

const agentSnapshotInterval = 20 * time.Second
const agentSnapshotFreshness = 30 * time.Second

func (s *Server) reconcileAgent(p *agentPeer) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.reconcile:
		case <-timer.C:
		}
		_, err := s.refreshAgent(p.ctx, p, nil)
		delay := agentSnapshotInterval
		if sshconnector.Code(err) == "busy" {
			delay = time.Second
		}
		timer.Reset(delay)
	}
}

func (s *Server) refreshAgent(ctx context.Context, p *agentPeer, audit *http.Request) (tmux.Inventory, error) {
	if !p.live() {
		return tmux.Inventory{}, &sshconnector.Failure{Code: "agent_offline"}
	}
	if !p.refresh.TryLock() {
		return tmux.Inventory{}, &sshconnector.Failure{Code: "busy"}
	}
	defer p.refresh.Unlock()

	p.mu.Lock()
	capabilityVersion := p.capabilityVersion
	p.mu.Unlock()
	inv, snapshotErr := s.agents.snapshot(ctx, p)
	if !p.live() {
		return tmux.Inventory{}, &sshconnector.Failure{Code: "agent_offline"}
	}
	if sshconnector.Code(snapshotErr) == "busy" {
		return tmux.Inventory{}, snapshotErr
	}
	code, state, result := "ok", "ONLINE", "success"
	if snapshotErr != nil {
		code, state = sshconnector.Code(snapshotErr), "DEGRADED"
		result = code
	} else if inv.StatusReason != "" {
		code = inv.StatusReason
	}
	inv.Capabilities.PTYAttach = snapshotErr == nil && s.agentTerminalEnabled() && p.terminal &&
		inv.Capabilities.ServerRunning && tmux.TerminalVersionSupported(inv.Capabilities.TmuxVersion)
	inv.Capabilities.ControlMode = snapshotErr == nil && p.controlEvents && inv.Capabilities.ServerRunning &&
		tmux.TerminalVersionSupported(inv.Capabilities.TmuxVersion)
	inv.AgentGeneration = p.generation

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return tmux.Inventory{}, err
	}
	defer tx.Rollback()
	var current bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_identities
		WHERE id=? AND host_id=? AND generation=? AND revoked_at IS NULL)`,
		p.id, p.hostID, p.generation).Scan(&current)
	if err != nil {
		return tmux.Inventory{}, err
	}
	if !current || !p.live() {
		return tmux.Inventory{}, &sshconnector.Failure{Code: "agent_offline"}
	}
	// A newer heartbeat can invalidate the result while the snapshot is in flight.
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.capabilityVersion != capabilityVersion || p.ctx.Err() != nil {
		return tmux.Inventory{}, &sshconnector.Failure{Code: "agent_reconciling"}
	}
	if err := storeObservation(ctx, tx, p.hostID, state, code, &inv, snapshotErr == nil); err != nil {
		return tmux.Inventory{}, err
	}
	if audit != nil {
		if err := auditTx(audit, tx, user(audit).ID, "host.probe", p.hostID, result); err != nil {
			return tmux.Inventory{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return tmux.Inventory{}, err
	}
	p.observed, p.inventoryReady = true, snapshotErr == nil
	p.inventoryEpoch, p.inventoryPTY = inv.Epoch, inv.Capabilities.PTYAttach
	p.observationReason, p.observationTime = code, time.Now()
	return inv, snapshotErr
}

func (s *Server) observeAgent(r *http.Request, h Host, audit bool) (tmux.Inventory, error) {
	var inv tmux.Inventory
	var err error
	p := s.agents.peer(h.ID)
	switch {
	case !s.agentEnabled():
		err = &sshconnector.Failure{Code: "agent_disabled"}
	case p == nil || p.socket != h.SocketName || p.osUser != h.OSUser:
		err = &sshconnector.Failure{Code: "agent_offline"}
	default:
		var auditRequest *http.Request
		if audit {
			auditRequest = r
		}
		inv, err = s.refreshAgent(r.Context(), p, auditRequest)
	}
	if err == nil {
		return inv, nil
	}
	code := sshconnector.Code(err)
	if audit && (code == "busy" || code == "agent_reconciling" || code == "agent_offline" || code == "agent_disabled") {
		if auditErr := s.audit(r, user(r).ID, "host.probe", h.ID, code); auditErr != nil {
			return tmux.Inventory{}, auditErr
		}
	}
	state := "UNREACHABLE"
	switch code {
	case "busy":
		return inv, err
	case "agent_reconciling":
		state = "RECONNECTING"
	case "agent_disabled", "agent_offline":
	default:
		if !isObservationFailure(err) {
			return inv, err
		}
		state = "DEGRADED"
	}
	return s.cachedObservation(r.Context(), h, state, code, err)
}

func (s *Server) applyAgentStatus(h Host) Host {
	if h.Transport != "agent" {
		return h
	}
	if !s.agentEnabled() {
		h.State, h.StatusReason, h.Stale = "UNREACHABLE", "agent_disabled", true
		return h
	}
	p := s.agents.peer(h.ID)
	if p == nil || !p.connectionAlive() {
		h.State, h.StatusReason, h.Stale = "UNREACHABLE", "agent_offline", true
		return h
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	h.AgentGeneration = p.generation
	if p.heartbeatSequence != 0 || p.instance == "" {
		seen := storage.Timestamp(p.lastHeartbeat)
		h.AgentLastHeartbeat = &seen
	}
	if !p.observed {
		h.State, h.StatusReason, h.Stale = "RECONNECTING", "agent_reconciling", true
		return h
	}
	observedAt := storage.Timestamp(p.observationTime)
	h.ObservedAt, h.StatusReason = &observedAt, p.observationReason
	if p.inventoryReady {
		h.State, h.Stale = "ONLINE", time.Since(p.observationTime) > agentSnapshotFreshness
	} else {
		h.State, h.Stale = "DEGRADED", true
	}
	return h
}

func (p *agentPeer) terminalReady(epoch string) bool {
	if !p.live() {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inventoryReady && p.inventoryPTY && time.Since(p.observationTime) <= agentSnapshotFreshness &&
		(epoch == "" || p.inventoryEpoch == epoch)
}

// An obsolete generation can never claim a browser viewer or persist a result.
func (p *agentPeer) current(ctx context.Context, db *sql.DB) (bool, error) {
	var current bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_identities
		WHERE id=? AND host_id=? AND generation=? AND revoked_at IS NULL)`,
		p.id, p.hostID, p.generation).Scan(&current)
	return current, err
}
