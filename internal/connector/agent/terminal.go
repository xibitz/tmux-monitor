package agent

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
)

func (b Binding) runTerminalOffer(parent context.Context, client *http.Client, offer agentproto.TerminalOffer) {
	u, err := ValidateHub(b.Hub)
	if err != nil {
		return
	}
	u.Scheme, u.Path = "wss", "/ws/v1/agent/terminal"
	setup, cancelSetup := context.WithDeadline(parent, offer.Deadline)
	defer cancelSetup()
	conn, _, err := websocket.Dial(setup, u.String(), &websocket.DialOptions{
		HTTPClient: client, Subprotocols: []string{agentproto.TerminalProtocol},
		HTTPHeader: http.Header{
			"Authorization":                 {"Bearer " + b.Credential},
			agentproto.TerminalTicketHeader: {offer.Ticket},
		},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != agentproto.TerminalProtocol {
		return
	}
	conn.SetReadLimit(agentproto.TerminalHeader + agentproto.MaxTerminalInput)
	lifetime, stop := context.WithTimeout(parent, 30*time.Minute)
	defer stop()
	interactive := offer.Mode == "interactive"
	t, status := openPTY(lifetime, setup, b.HostID, b.Socket, offer.Target, offer.Epoch, interactive)
	event := agentproto.TerminalEvent{Type: agentproto.TerminalReady, Status: status}
	if t != nil {
		event.Cols, event.Rows = t.policy.Cols, t.policy.Rows
	}
	if err := agentproto.Write(setup, conn, event); err != nil || t == nil {
		if t != nil {
			t.close()
		}
		return
	}
	cancelSetup()
	runPTYStream(lifetime, conn, t, interactive)
}

func runPTYStream(parent context.Context, conn *websocket.Conn, t *localPTY, interactive bool) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	outputDone, inputDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(outputDone)
		data := make([]byte, agentproto.MaxTerminalOutput)
		var sequence uint64
		for ctx.Err() == nil {
			n, err := t.master.Read(data)
			if n > 0 && ctx.Err() == nil {
				sequence++
				if writeTerminalFrame(parent, conn, agentproto.TerminalOutput, sequence, data[:n]) != nil {
					cancel(errors.New("slow_consumer"))
					return
				}
			}
			if err != nil {
				cancel(errors.New("agent_closed"))
				return
			}
		}
	}()
	go func() {
		defer close(inputDone)
		cancel(errors.New(readPTYInput(parent, ctx, conn, t, interactive)))
	}()

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-tick.C:
			check, stop := context.WithTimeout(ctx, agentproto.TerminalIOTimeout)
			status := t.check(check)
			stop()
			if status != "ok" {
				cancel(errors.New(status))
			}
		}
	}
	t.close()
	<-outputDone
	// A close acknowledgement only follows termination of our attach client.
	// It does not affect the tmux server or processes in the session.
	status := context.Cause(ctx).Error()
	if !agentproto.ValidTerminalStatus(status) {
		status = "agent_closed"
	}
	write, stop := context.WithTimeout(parent, agentproto.TerminalIOTimeout)
	_ = agentproto.Write(write, conn, agentproto.TerminalEvent{Type: agentproto.TerminalClosed, Status: status})
	stop()
	_ = conn.CloseNow()
	<-inputDone
}

func readPTYInput(parent, ctx context.Context, conn *websocket.Conn, t *localPTY, interactive bool) string {
	var sequence uint64
	for ctx.Err() == nil {
		kind, data, err := conn.Read(parent)
		if err != nil {
			return "agent_closed"
		}
		if kind == websocket.MessageText {
			var event agentproto.TerminalEvent
			if agentproto.Decode(data, &event) == nil && event == (agentproto.TerminalEvent{Type: agentproto.TerminalDetach}) {
				return "detached"
			}
			return "input_invalid"
		}
		if kind != websocket.MessageBinary || !interactive {
			return "input_forbidden"
		}
		direction, next, payload, ok := agentproto.ParseTerminalFrame(data)
		if !ok || direction != agentproto.TerminalInput || next != sequence+1 || ctx.Err() != nil {
			return "input_invalid"
		}
		if t.master.SetWriteDeadline(time.Now().Add(agentproto.TerminalIOTimeout)) != nil {
			return "input_uncertain"
		}
		n, err := t.master.Write(payload)
		if err != nil || n != len(payload) {
			return "input_uncertain"
		}
		sequence = next
		if writeTerminalFrame(parent, conn, agentproto.TerminalInputAck, sequence, nil) != nil {
			return "input_uncertain"
		}
	}
	return "agent_closed"
}

func writeTerminalFrame(ctx context.Context, conn *websocket.Conn, direction byte, sequence uint64, data []byte) error {
	write, stop := context.WithTimeout(ctx, agentproto.TerminalIOTimeout)
	defer stop()
	return conn.Write(write, websocket.MessageBinary, agentproto.TerminalFrame(direction, sequence, data))
}
