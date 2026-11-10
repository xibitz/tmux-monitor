package sshconnector

import (
	"context"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func (m *Manager) RenameWindow(ctx context.Context, h Host, record Record, windowID string, request tmux.RenameWindowRequest, authorize func(context.Context) error) (string, error) {
	if !tmux.ValidRenameWindow(windowID, request) {
		return "", fail("invalid_request")
	}
	return m.executeGuardedMutation(ctx, h, record, windowID, "MTM_WINDOW_RENAMED\n", func(output []byte) (string, error) {
		return tmux.RenameWindowCommand(h.ID, h.Socket, windowID, request, output)
	}, authorize)
}
