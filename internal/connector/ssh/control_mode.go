package sshconnector

import (
	"context"
	"io"
	"strings"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func (m *Manager) WatchInventory(ctx context.Context, h Host, record Record, changed func()) error {
	args, err := tmux.ControlArgs(h.Socket)
	if err != nil {
		return err
	}
	for i := range args {
		args[i] = tmux.ShellQuote(args[i])
	}
	client, _, err := m.connect(ctx, h, record)
	if err != nil {
		return err
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	version, _, err := run(ctx, client, "LC_ALL=C tmux -V", 1024)
	if err != nil || !tmux.TerminalVersionSupported(strings.TrimPrefix(strings.TrimSpace(string(version)), "tmux ")) {
		return fail("terminal_unsupported")
	}
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	output, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	input, err := session.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	session.Stderr = io.Discard
	if err := session.Start("LC_ALL=C env -u TMUX -u TMUX_PANE " + strings.Join(args, " ")); err != nil {
		return err
	}
	return tmux.ReadControl(output, func(event tmux.ControlEvent) {
		if event.Kind == "inventory.changed" || event.Kind == "command.error" {
			changed()
		}
	})
}
