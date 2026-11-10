package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func (b Binding) controlEvents(ctx context.Context, conn *websocket.Conn, generation int64) {
	changes := make(chan struct{}, 1)
	watchCtx, stop := context.WithCancel(ctx)
	defer stop()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		for watchCtx.Err() == nil {
			watchLocalInventory(watchCtx, b.Socket, changes)
			timer := time.NewTimer(20 * time.Second)
			select {
			case <-watchCtx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}()
	defer func() { stop(); <-watchDone }()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var sequence uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			select {
			case <-changes:
			default:
				continue
			}
			sequence++
			event := agentproto.InventoryEvent{Type: agentproto.InventoryChanged, AgentID: b.AgentID, Generation: generation, Sequence: sequence}
			write, done := context.WithTimeout(ctx, agentproto.HandshakeTimeout)
			err := agentproto.Write(write, conn, event)
			done()
			if err != nil {
				return
			}
		}
	}
}

func watchLocalInventory(ctx context.Context, socket string, changes chan<- struct{}) {
	probe, done := context.WithTimeout(ctx, 2*time.Second)
	version, status := tmuxVersion(probe)
	done()
	if status != "ok" || !tmux.TerminalVersionSupported(version) {
		return
	}
	args, err := tmux.ControlArgs(socket)
	if err != nil {
		return
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		return key == "TMUX" || key == "TMUX_PANE"
	}), "LC_ALL=C")
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	defer output.Close()
	stopRead := context.AfterFunc(ctx, func() { _ = output.Close() })
	defer stopRead()
	if err := cmd.Start(); err != nil {
		return
	}
	mark := func() {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
	mark() // New stream, always reconcile from a fresh snapshot.
	_ = tmux.ReadControl(output, func(event tmux.ControlEvent) {
		if event.Kind == "inventory.changed" || event.Kind == "command.error" {
			mark()
		}
	})
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	mark()
}
