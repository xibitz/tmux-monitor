package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

type interactiveOpener interface {
	OpenInteractiveTerminal(context.Context, sshconnector.Host, sshconnector.Record, string, string) (sshconnector.Terminal, error)
}
type terminalViewer struct {
	terminalTicket
	Cancel    context.CancelCauseFunc
	Done      chan struct{}
	Switching bool
}

func (s *Server) issueTerminalTicket(t terminalTicket, ttl time.Duration) (string, bool) {
	t.Expires = time.Now().Add(ttl)
	raw := storage.ID("") + storage.ID("")
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	count := 0
	for hash, ticket := range s.tickets {
		if time.Now().After(ticket.Expires) {
			delete(s.tickets, hash)
		} else if ticket.TokenHash == t.TokenHash {
			count++
		}
	}
	if s.terminalClosing || len(s.tickets) >= 64 || count >= 8 {
		return "", false
	}
	s.tickets[identity.TokenHash(raw)] = t
	return raw, true
}

func (s *Server) canWriteTerminal(ctx context.Context, r *http.Request, t terminalTicket) bool {
	if _, ok := s.terminal.(interactiveOpener); !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	u, err := identity.Authenticate(ctx, s.db, token(r))
	if err != nil || u.ID != t.UserID {
		return false
	}
	if u.IsAdmin {
		return true
	}
	var count int
	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM host_access WHERE user_id=? AND host_id=? AND action='terminal:write'", u.ID, t.HostID).Scan(&count)
	return err == nil && count == 1
}

func (s *Server) terminalModeTicket(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.TerminalEnabled {
		s.fail(w, r, 503, "terminal_disabled", "Viewer belum diaktifkan.")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if !s.decode(w, r, &body) {
		return
	}
	if body.Mode != "interactive" && body.Mode != "read_only" {
		s.fail(w, r, 422, "invalid_request", "Mode viewer tidak valid.")
		return
	}
	if _, ok := s.terminal.(interactiveOpener); !ok {
		s.fail(w, r, 503, "interactive_unavailable", "Input interaktif belum tersedia pada konektor ini.")
		return
	}
	s.viewMu.Lock()
	v, ok := s.viewers[r.PathValue("viewer")]
	if !ok || v.TokenHash != identity.TokenHash(token(r)) || v.UserID != user(r).ID || v.Cancel == nil {
		s.viewMu.Unlock()
		s.fail(w, r, 404, "not_found", "Viewer aktif tidak ditemukan.")
		return
	}
	if v.Switching || v.Mode == body.Mode {
		s.viewMu.Unlock()
		s.fail(w, r, 409, "conflict", "Mode viewer sedang berubah atau sudah sesuai.")
		return
	}
	v.Switching = true
	t := v.terminalTicket
	s.viewMu.Unlock()
	defer func() { s.viewMu.Lock(); v.Switching = false; s.viewMu.Unlock() }()
	t.Mode = body.Mode
	if err := s.checkViewer(r.Context(), r, t); err != nil {
		if err = s.terminalAudit(r, t, "terminal.mode", "denied"); err != nil {
			s.internal(w, r)
			return
		}
		s.fail(w, r, 403, "forbidden", "Izin mengirim input dan akses host diperlukan untuk mengaktifkan input.")
		return
	}
	if err := s.terminalAudit(r, t, "terminal.mode", "requested_"+body.Mode); err != nil {
		s.internal(w, r)
		return
	}
	v.Cancel(errors.New("mode_changed"))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-v.Done:
	case <-ctx.Done():
		s.fail(w, r, 503, "transition_failed", "Viewer lama belum selesai dilepas. Buka ulang dalam read-only.")
		return
	}
	if err := s.checkViewer(ctx, r, t); err != nil {
		s.fail(w, r, 403, "forbidden", "Akses berubah saat pergantian mode. Buka ulang dalam read-only.")
		return
	}
	raw, allowed := s.issueTerminalTicket(t, 15*time.Second)
	if !allowed {
		s.fail(w, r, 429, "busy", "Batas tiket tercapai. Buka ulang dalam read-only setelah menunggu.")
		return
	}
	s.json(w, 201, map[string]any{"ticket": raw, "expiresIn": 15})
}
