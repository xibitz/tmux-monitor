package agent

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
)

func (b Binding) sendHeartbeats(ctx context.Context, conn *websocket.Conn, generation int64, interval time.Duration, acks <-chan uint64) {
	lease := agentproto.MissedHeartbeats * interval
	lastAck := time.Now()
	expire := time.NewTimer(lease)
	defer expire.Stop()
	send := time.NewTimer(0)
	defer send.Stop()
	var sent, acknowledged uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-expire.C:
			return
		case sequence := <-acks:
			if sequence != acknowledged+1 || sequence > sent || time.Since(lastAck) >= lease {
				return
			}
			acknowledged, lastAck = sequence, time.Now()
			expire.Reset(lease)
		case <-send.C:
			if time.Since(lastAck) >= lease {
				return
			}
			next := time.Now().Add(interval)
			probe, cancel := context.WithTimeout(ctx, 2*time.Second)
			version, _ := tmuxVersion(probe)
			cancel()
			sent++
			write, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := agentproto.Write(write, conn, agentproto.Heartbeat{
				Type: agentproto.HeartbeatType, AgentID: b.AgentID, Generation: generation, Sequence: sent, Tmux: version,
			})
			cancel()
			if err != nil {
				return
			}
			send.Reset(max(0, time.Until(next)))
		}
	}
}
