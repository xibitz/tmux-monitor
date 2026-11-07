package sshconnector

import (
	"context"
	"errors"
	"strings"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func (m *Manager) RenameSession(ctx context.Context, h Host, record Record, session string, request tmux.RenameRequest, authorize func(context.Context) error) error {
	if !tmux.ValidRename(session, request) || authorize == nil {
		return fail("invalid_request")
	}
	m.mu.Lock()
	if m.busy[h.ID] {
		m.mu.Unlock()
		return fail("busy")
	}
	select {
	case m.gate <- struct{}{}:
	default:
		m.mu.Unlock()
		return fail("busy")
	}
	m.busy[h.ID] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.busy, h.ID); m.mu.Unlock(); <-m.gate }()
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	client, _, err := m.connect(ctx, h, record)
	if err != nil {
		return err
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	version, _, err := run(ctx, client, "LC_ALL=C tmux -V", 1024)
	if err != nil {
		return versionError(err)
	}
	if strings.TrimSpace(string(version)) != "tmux 3.8" {
		return fail("tmux_unsupported")
	}
	snapshot, err := tmux.SnapshotCommand(h.Socket)
	if err != nil {
		return fail("invalid_request")
	}
	output, stderr, err := run(ctx, client, snapshot, tmux.MaxOutput)
	if err != nil {
		if Code(err) == "command_failed" && (noServer(stderr) || strings.TrimSpace(string(stderr)) == "no current target") {
			return fail("conflict")
		}
		return err
	}
	command, err := tmux.RenameCommand(h.ID, h.Socket, session, request, output)
	if err != nil {
		if errors.Is(err, tmux.ErrChanged) {
			return fail("conflict")
		}
		if errors.Is(err, tmux.ErrNotFound) {
			return fail("not_found")
		}
		if err.Error() == "tmux_unsupported" {
			return fail("tmux_unsupported")
		}
		return fail("inventory_invalid")
	}
	if err = authorize(ctx); err != nil {
		return fail("access_revoked")
	}
	// Once dispatched, a missing/ambiguous reply must never trigger a retry.
	output, stderr, err = run(ctx, client, command, 1024)
	if err != nil {
		if Code(err) == "command_failed" && strings.HasPrefix(strings.TrimSpace(string(stderr)), "duplicate session:") {
			return fail("duplicate_name")
		}
		return fail("operation_uncertain")
	}
	switch string(output) {
	case "MTM_RENAMED\n":
		return nil
	case "MTM_CONFLICT\n":
		return fail("conflict")
	default:
		return fail("operation_uncertain")
	}
}
