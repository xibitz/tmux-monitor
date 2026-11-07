package sshconnector

import (
	"context"
	"errors"
	"strings"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

func (m *Manager) CreateSession(ctx context.Context, h Host, record Record, r tmux.CreateSessionRequest, authorize func(context.Context) error) (string, error) {
	if !tmux.ValidCreateSession(r) {
		return "", fail("invalid_request")
	}
	return m.executeSessionLifecycle(ctx, h, record, "", true, func(output []byte) (string, error) { return tmux.CreateSessionCommand(h.ID, h.Socket, r, output) }, authorize)
}
func (m *Manager) KillSession(ctx context.Context, h Host, record Record, session string, r tmux.KillSessionRequest, authorize func(context.Context) error) (string, error) {
	if !tmux.ValidKillSession(session, r) {
		return "", fail("invalid_request")
	}
	return m.executeSessionLifecycle(ctx, h, record, session, false, func(output []byte) (string, error) {
		return tmux.KillSessionCommand(h.ID, h.Socket, session, r, output)
	}, authorize)
}

// Kept separate from the verified rename implementation until lifecycle tests
// qualify these new catalogs. All operations share the same bounded host gate.
func (m *Manager) executeSessionLifecycle(ctx context.Context, h Host, record Record, target string, creating bool, catalog func([]byte) (string, error), authorize func(context.Context) error) (string, error) {
	if authorize == nil {
		return "", fail("invalid_request")
	}
	m.mu.Lock()
	if m.busy[h.ID] {
		m.mu.Unlock()
		return "", fail("busy")
	}
	select {
	case m.gate <- struct{}{}:
	default:
		m.mu.Unlock()
		return "", fail("busy")
	}
	m.busy[h.ID] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.busy, h.ID); m.mu.Unlock(); <-m.gate }()
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	client, _, err := m.connect(ctx, h, record)
	if err != nil {
		return "", err
	}
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	version, _, err := run(ctx, client, "LC_ALL=C tmux -V", 1024)
	if err != nil {
		return "", versionError(err)
	}
	if strings.TrimSpace(string(version)) != "tmux 3.8" {
		return "", fail("tmux_unsupported")
	}
	snapshot, err := tmux.SnapshotCommand(h.Socket)
	if err != nil {
		return "", fail("invalid_request")
	}
	output, stderr, err := run(ctx, client, snapshot, tmux.MaxOutput)
	if err != nil && Code(err) == "command_failed" && strings.TrimSpace(string(stderr)) == "no current target" {
		empty, catalogErr := tmux.EmptySnapshotCommand(h.Socket)
		if catalogErr != nil {
			return "", fail("invalid_request")
		}
		output, stderr, err = run(ctx, client, empty, tmux.MaxOutput)
	}
	if err != nil {
		if Code(err) == "command_failed" && noServer(stderr) {
			if creating {
				return "", fail("server_required")
			}
			return "", fail("conflict")
		}
		return "", err
	}
	command, err := catalog(output)
	if err != nil {
		switch {
		case errors.Is(err, tmux.ErrChanged):
			return "", fail("conflict")
		case errors.Is(err, tmux.ErrNotFound):
			return "", fail("not_found")
		case errors.Is(err, tmux.ErrLifecycleLimit):
			return "", fail("mutation_limit")
		case errors.Is(err, tmux.ErrLifecycleDuplicate):
			return "", fail("duplicate_name")
		case errors.Is(err, tmux.ErrLifecycleUnsupported):
			return "", fail("tmux_unsupported")
		default:
			return "", fail("inventory_invalid")
		}
	}
	if err = authorize(ctx); err != nil {
		return "", fail("access_revoked")
	}
	output, stderr, err = run(ctx, client, command, 1024)
	if err != nil {
		if creating && Code(err) == "command_failed" && strings.HasPrefix(strings.TrimSpace(string(stderr)), "duplicate session:") {
			return "", fail("duplicate_name")
		}
		return "", fail("operation_uncertain")
	}
	if string(output) == "MTM_CONFLICT\n" {
		return "", fail("conflict")
	}
	if creating {
		id, err := tmux.CreatedSessionID(output)
		if err != nil {
			return "", fail("operation_uncertain")
		}
		return id, nil
	}
	if string(output) != "MTM_KILLED\n" {
		return "", fail("operation_uncertain")
	}
	return target, nil
}
