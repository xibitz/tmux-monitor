package httpapi

import (
	"context"
	"time"

	"github.com/xibitz/tmux-monitor/internal/agentproto"
)

func (p *agentPeer) heartbeatDeadline() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastHeartbeat.Add(agentproto.MissedHeartbeats * p.heartbeat)
}

func (p *agentPeer) connectionAlive() bool {
	return p.ctx.Err() == nil && (p.instance == "" || time.Now().Before(p.heartbeatDeadline()))
}

func (p *agentPeer) live() bool {
	select {
	case <-p.ready:
		return p.connectionAlive()
	default:
		return false
	}
}

func (p *agentPeer) receiveHeartbeat(data []byte) bool {
	var beat agentproto.Heartbeat
	if agentproto.Decode(data, &beat) != nil || beat.AgentID != p.id || beat.Generation != p.generation ||
		(beat.Tmux != "" && !agentproto.ValidTmuxVersion(beat.Tmux)) {
		return false
	}
	now := time.Now()
	p.mu.Lock()
	if beat.Sequence == 0 || beat.Sequence != p.heartbeatSequence+1 ||
		!now.Before(p.lastHeartbeat.Add(agentproto.MissedHeartbeats*p.heartbeat)) {
		p.mu.Unlock()
		return false
	}
	p.heartbeatSequence, p.lastHeartbeat = beat.Sequence, now
	if beat.Tmux != p.tmuxVersion {
		p.tmuxVersion = beat.Tmux
		p.capabilityVersion++
		p.observed, p.inventoryReady = false, false
		select {
		case p.reconcile <- struct{}{}:
		default:
		}
	}
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Second)
	defer cancel()
	return agentproto.Write(ctx, p.conn, agentproto.HeartbeatAck{
		Type: agentproto.HeartbeatAckType, AgentID: p.id, Generation: p.generation, Sequence: beat.Sequence,
	}) == nil
}

func (p *agentPeer) pingLegacy() {
	tick := time.NewTicker(p.heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-tick.C:
			ctx, cancel := context.WithTimeout(p.ctx, agentproto.MissedHeartbeats*p.heartbeat)
			err := p.conn.Ping(ctx)
			cancel()
			if err != nil {
				p.cancel()
				return
			}
			p.mu.Lock()
			p.lastHeartbeat = time.Now()
			p.mu.Unlock()
		}
	}
}
