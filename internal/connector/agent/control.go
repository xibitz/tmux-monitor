package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
)

func (b Binding) controlLoop(parent context.Context, conn *websocket.Conn, client *http.Client, generation int64, heartbeat time.Duration, options RunOptions, ack agentproto.HelloAck) error {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()
	if options.ControlEvents && ack.ControlEvents {
		wg.Go(func() { b.controlEvents(ctx, conn, generation) })
	}
	if options.HookSocket != "" && ack.CodingHooks {
		wg.Go(func() { b.listenHooks(ctx, conn, generation, options.HookSocket) })
	}
	inventory := make(chan struct{}, 1)
	viewers := make(chan struct{}, 4)
	acknowledgements := make(chan uint64, agentproto.MissedHeartbeats)
	wg.Go(func() {
		defer cancel()
		b.sendHeartbeats(ctx, conn, generation, heartbeat, acknowledgements)
	})
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			if parent.Err() != nil {
				return nil
			}
			return errors.New("Agent connection closed")
		}
		var envelope struct{ Type string }
		if kind != websocket.MessageText || json.Unmarshal(data, &envelope) != nil {
			return errors.New("invalid Agent RPC")
		}
		switch envelope.Type {
		case agentproto.HeartbeatAckType:
			var ack agentproto.HeartbeatAck
			if agentproto.Decode(data, &ack) != nil || ack.AgentID != b.AgentID || ack.Generation != generation || ack.Sequence == 0 {
				return errors.New("invalid heartbeat acknowledgement")
			}
			select {
			case acknowledgements <- ack.Sequence:
			default:
				return errors.New("too many heartbeat acknowledgements")
			}
		case agentproto.Snapshot:
			var req agentproto.Request
			if agentproto.Decode(data, &req) != nil || req.AgentID != b.AgentID || req.Generation != generation ||
				!agentproto.ValidRequestID(req.RequestID) || req.Deadline.IsZero() ||
				req.Deadline.After(time.Now().Add(agentproto.SnapshotTimeout+5*time.Second)) {
				return errors.New("invalid inventory RPC")
			}
			select {
			case inventory <- struct{}{}:
			default:
				return errors.New("overlapping inventory RPC")
			}
			wg.Go(func() {
				operation, done := context.WithDeadline(ctx, req.Deadline)
				result := snapshot(operation, b.Socket)
				done()
				result.Type, result.RequestID, result.AgentID, result.Generation = agentproto.SnapshotReply, req.RequestID, b.AgentID, generation
				// The computation is finished. The next request may arrive as soon
				// as this response is visible to the hub.
				<-inventory
				write, done := context.WithTimeout(ctx, agentproto.HandshakeTimeout)
				defer done()
				if agentproto.Write(write, conn, result) != nil {
					cancel()
				}
			})
		case agentproto.TerminalOpen:
			var offer agentproto.TerminalOffer
			if !options.Terminal || agentproto.Decode(data, &offer) != nil || !offer.Valid(b.AgentID, generation, time.Now()) {
				return errors.New("invalid terminal offer")
			}
			if !time.Now().Before(offer.Deadline) {
				continue
			}
			select {
			case viewers <- struct{}{}:
				wg.Go(func() {
					defer func() { <-viewers }()
					b.runTerminalOffer(ctx, client, offer)
				})
			default:
				// The hub's own setup deadline expires without opening a PTY.
			}
		default:
			return errors.New("unsupported Agent RPC")
		}
	}
}
