package sshconnector

import (
	"context"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func (m *Manager) KillWindow(ctx context.Context, h Host, record Record, windowID string, request tmux.KillWindowRequest, authorize func(context.Context) error) (string, error) {
	if !tmux.ValidKillWindow(windowID, request) {
		return "", fail("invalid_request")
	}
	return m.executeGuardedMutation(ctx, h, record, false, func(output []byte) (string, error) {
		return tmux.KillWindowCommand(h.ID, h.Socket, windowID, request, output)
	}, fixedMutationResult("MTM_WINDOW_KILLED\n", windowID), authorize)
}

func (m *Manager) SplitPane(ctx context.Context, h Host, record Record, paneID string, request tmux.SplitPaneRequest, authorize func(context.Context) error) (string, error) {
	if !tmux.ValidSplitPane(paneID, request) {
		return "", fail("invalid_request")
	}
	return m.executeGuardedMutation(ctx, h, record, false, func(output []byte) (string, error) {
		return tmux.SplitPaneCommand(h.ID, h.Socket, paneID, request, output)
	}, tmux.CreatedPaneID, authorize)
}

func (m *Manager) KillPane(ctx context.Context, h Host, record Record, paneID string, request tmux.KillPaneRequest, authorize func(context.Context) error) (string, error) {
	if !tmux.ValidKillPane(paneID, request) {
		return "", fail("invalid_request")
	}
	return m.executeGuardedMutation(ctx, h, record, false, func(output []byte) (string, error) {
		return tmux.KillPaneCommand(h.ID, h.Socket, paneID, request, output)
	}, fixedMutationResult("MTM_PANE_KILLED\n", paneID), authorize)
}
