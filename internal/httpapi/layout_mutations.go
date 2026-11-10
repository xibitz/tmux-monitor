package httpapi

import (
	"context"
	"net/http"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

const windowKillAction = "tmux.window.kill"
const paneSplitAction = "tmux.pane.split"
const paneKillAction = "tmux.pane.kill"

type layoutMutator interface {
	KillWindow(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.KillWindowRequest, func(context.Context) error) (string, error)
	SplitPane(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.SplitPaneRequest, func(context.Context) error) (string, error)
	KillPane(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.KillPaneRequest, func(context.Context) error) (string, error)
}

func (s *Server) layoutMutationsEnabled() bool {
	return s.cfg.MutationsEnabled && s.cfg.LayoutMutationsEnabled && s.layoutMutations != nil
}

func (s *Server) decodeLayoutMutation(w http.ResponseWriter, r *http.Request, body any) bool {
	if !s.layoutMutationsEnabled() {
		s.fail(w, r, 503, "layout_mutations_disabled", "Operasi layout tmux belum diaktifkan.")
		return false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || !operationKey.MatchString(r.Header.Get("Idempotency-Key")) {
		s.fail(w, r, 422, "invalid_request", "URL atau kunci operasi layout tidak valid.")
		return false
	}
	return s.decode(w, r, body)
}

func (s *Server) killWindow(w http.ResponseWriter, r *http.Request) {
	var body tmux.KillWindowRequest
	if !s.decodeLayoutMutation(w, r, &body) {
		return
	}
	id := r.PathValue("wid")
	if !tmux.ValidKillWindow(id, body) {
		s.fail(w, r, 422, "invalid_request", "Target, revision, atau konfirmasi ID window tidak valid.")
		return
	}
	execute := func(ctx context.Context, h sshconnector.Host, record sshconnector.Record, authorize func(context.Context) error) (string, error) {
		return s.layoutMutations.KillWindow(ctx, h, record, id, body, authorize)
	}
	s.executeLifecycleRequest(w, r, windowKillAction, "tmux:kill", body.Epoch, id, body, 200, execute)
}

func (s *Server) splitPane(w http.ResponseWriter, r *http.Request) {
	var body tmux.SplitPaneRequest
	if !s.decodeLayoutMutation(w, r, &body) {
		return
	}
	id := r.PathValue("pid")
	if !tmux.ValidSplitPane(id, body) {
		s.fail(w, r, 422, "invalid_request", "Target, revision, arah split, atau konfirmasi pane tidak valid.")
		return
	}
	execute := func(ctx context.Context, h sshconnector.Host, record sshconnector.Record, authorize func(context.Context) error) (string, error) {
		return s.layoutMutations.SplitPane(ctx, h, record, id, body, authorize)
	}
	s.executeLifecycleRequest(w, r, paneSplitAction, "tmux:write", body.Epoch, id, body, 201, execute)
}

func (s *Server) killPane(w http.ResponseWriter, r *http.Request) {
	var body tmux.KillPaneRequest
	if !s.decodeLayoutMutation(w, r, &body) {
		return
	}
	id := r.PathValue("pid")
	if !tmux.ValidKillPane(id, body) {
		s.fail(w, r, 422, "invalid_request", "Target, revision, atau konfirmasi ID pane tidak valid.")
		return
	}
	execute := func(ctx context.Context, h sshconnector.Host, record sshconnector.Record, authorize func(context.Context) error) (string, error) {
		return s.layoutMutations.KillPane(ctx, h, record, id, body, authorize)
	}
	s.executeLifecycleRequest(w, r, paneKillAction, "tmux:kill", body.Epoch, id, body, 200, execute)
}
