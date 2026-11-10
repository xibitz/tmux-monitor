package httpapi

import (
	"context"
	"net/http"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

const windowRenameAction = "tmux.window.rename"
const windowCreateAction = "tmux.window.create"

type windowMutator interface {
	RenameWindow(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.RenameWindowRequest, func(context.Context) error) (string, error)
	CreateWindow(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.CreateWindowRequest, func(context.Context) error) (string, error)
}

func (s *Server) windowMutationsEnabled() bool {
	return s.cfg.MutationsEnabled && s.cfg.WindowMutationsEnabled && s.windowMutations != nil
}

func (s *Server) createWindow(w http.ResponseWriter, r *http.Request) {
	if !s.windowMutationsEnabled() {
		s.fail(w, r, 503, "window_mutations_disabled", "Buat window belum diaktifkan.")
		return
	}
	var body tmux.CreateWindowRequest
	if !s.decode(w, r, &body) {
		return
	}
	sessionID := r.PathValue("sid")
	if r.URL.RawQuery != "" || !operationKey.MatchString(r.Header.Get("Idempotency-Key")) || !tmux.ValidCreateWindow(sessionID, body) {
		s.fail(w, r, 422, "invalid_request", "Nama atau target session tidak valid. Periksa inventaris kembali.")
		return
	}
	execute := func(ctx context.Context, host sshconnector.Host, record sshconnector.Record, authorize func(context.Context) error) (string, error) {
		return s.windowMutations.CreateWindow(ctx, host, record, sessionID, body, authorize)
	}
	s.executeLifecycleRequest(w, r, windowCreateAction, "tmux:write", body.Epoch, sessionID, body, 201, execute)
}

func (s *Server) renameWindow(w http.ResponseWriter, r *http.Request) {
	if !s.windowMutationsEnabled() {
		s.fail(w, r, 503, "window_mutations_disabled", "Ubah nama window belum diaktifkan.")
		return
	}
	var body tmux.RenameWindowRequest
	if !s.decode(w, r, &body) {
		return
	}
	windowID := r.PathValue("wid")
	if r.URL.RawQuery != "" || !operationKey.MatchString(r.Header.Get("Idempotency-Key")) || !tmux.ValidRenameWindow(windowID, body) {
		s.fail(w, r, 422, "invalid_request", "Nama atau target window tidak valid. Periksa inventaris kembali.")
		return
	}
	execute := func(ctx context.Context, host sshconnector.Host, record sshconnector.Record, authorize func(context.Context) error) (string, error) {
		return s.windowMutations.RenameWindow(ctx, host, record, windowID, body, authorize)
	}
	s.executeLifecycleRequest(w, r, windowRenameAction, "tmux:write", body.Epoch, windowID, body, 200, execute)
}
