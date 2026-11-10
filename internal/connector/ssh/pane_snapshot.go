package sshconnector

import (
	"context"
	"errors"
	"strings"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func (m *Manager) CapturePane(ctx context.Context, h Host, record Record, paneID string, request tmux.PaneSnapshotRequest, authorize func(context.Context) error) (tmux.PaneSnapshot, error) {
	var empty tmux.PaneSnapshot
	if !tmux.ValidPaneSnapshot(paneID, request) || authorize == nil {
		return empty, fail("invalid_request")
	}
	m.mu.Lock()
	if m.busy[h.ID] {
		m.mu.Unlock()
		return empty, fail("busy")
	}
	select {
	case m.gate <- struct{}{}:
	default:
		m.mu.Unlock()
		return empty, fail("busy")
	}
	m.busy[h.ID] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.busy, h.ID); m.mu.Unlock(); <-m.gate }()
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	client, _, err := m.connect(ctx, h, record)
	if err != nil {
		return empty, err
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	version, _, err := run(ctx, client, "LC_ALL=C tmux -V", 1024)
	if err != nil {
		return empty, versionError(err)
	}
	if strings.TrimSpace(string(version)) != "tmux 3.8" {
		return empty, fail("tmux_unsupported")
	}
	snapshot, err := tmux.SnapshotCommand(h.Socket)
	if err != nil {
		return empty, fail("invalid_request")
	}
	output, stderr, err := run(ctx, client, snapshot, tmux.MaxOutput)
	if err != nil {
		if Code(err) == "command_failed" && (noServer(stderr) || strings.TrimSpace(string(stderr)) == "no current target") {
			return empty, fail("not_found")
		}
		return empty, err
	}
	command, err := tmux.PaneSnapshotCommand(h.ID, h.Socket, paneID, request, output)
	if err != nil {
		switch {
		case errors.Is(err, tmux.ErrChanged):
			return empty, fail("conflict")
		case errors.Is(err, tmux.ErrNotFound):
			return empty, fail("not_found")
		case errors.Is(err, tmux.ErrLifecycleUnsupported):
			return empty, fail("tmux_unsupported")
		case errors.Is(err, tmux.ErrLifecycleLimit):
			return empty, fail("pane_snapshot_limit")
		default:
			return empty, fail("inventory_invalid")
		}
	}
	if err = authorize(ctx); err != nil {
		return empty, fail("access_revoked")
	}
	output, stderr, err = run(ctx, client, command, tmux.MaxPaneSnapshotRaw)
	if err != nil {
		if Code(err) == "command_failed" && noServer(stderr) {
			return empty, fail("conflict")
		}
		return empty, err
	}
	result, err := tmux.ParsePaneSnapshot(output)
	if errors.Is(err, tmux.ErrChanged) {
		return empty, fail("conflict")
	}
	if err != nil {
		return empty, fail("pane_snapshot_invalid")
	}
	return result, nil
}
