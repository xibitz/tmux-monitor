package httpapi

import (
	"context"
	"net/http"
	"time"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type paneCapturer interface {
	CapturePane(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.PaneSnapshotRequest, func(context.Context) error) (tmux.PaneSnapshot, error)
}

func (s *Server) capturePane(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.PaneSnapshotEnabled || s.paneSnapshots == nil {
		s.fail(w, r, 503, "pane_snapshot_disabled", "Snapshot pane belum diaktifkan.")
		return
	}
	var request tmux.PaneSnapshotRequest
	if !s.decode(w, r, &request) {
		return
	}
	paneID := r.PathValue("pid")
	if r.URL.RawQuery != "" || !tmux.ValidPaneSnapshot(paneID, request) {
		s.fail(w, r, 422, "invalid_request", "Target pane tidak valid. Periksa inventaris kembali.")
		return
	}
	host, record, err := s.terminalAccess(r.Context(), user(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, 404, "not_found", "Host SSH tidak tersedia atau tidak diizinkan.")
		return
	}
	checkAccess := func(ctx context.Context) error {
		u, err := identity.Authenticate(ctx, s.db, token(r))
		if err != nil || u.ID != user(r).ID {
			return identity.ErrCredentials
		}
		current, credential, err := s.terminalAccess(ctx, u, host.ID)
		if err != nil || credential.KeyRef != record.KeyRef || current.SocketName != host.SocketName || current.OSUser != host.OSUser || current.TailnetAddress != host.TailnetAddress {
			return identity.ErrCredentials
		}
		return nil
	}
	// Only operation metadata is passed to the audit recorder, never pane text.
	audit := func(ctx context.Context, result string) error {
		_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(actor,action,host_id,target,result,request_id,at) VALUES (?,?,?,?,?,?,?)`, user(r).ID, "tmux.pane.snapshot", host.ID, request.Epoch+":"+paneID, result, requestID(r), storage.Now())
		return err
	}
	if err = audit(r.Context(), "intent"); err != nil {
		s.internal(w, r)
		return
	}
	snapshot, err := s.paneSnapshots.CapturePane(r.Context(), sshconnector.Host{ID: host.ID, Address: host.TailnetAddress, User: host.OSUser, Socket: host.SocketName}, record, paneID, request, checkAccess)
	capturedAt := storage.Now()
	if err == nil && checkAccess(r.Context()) != nil {
		err = &sshconnector.Failure{Code: "access_revoked"}
	}
	code := "success"
	if err != nil {
		code = sshconnector.Code(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if auditErr := audit(ctx, code); auditErr != nil {
		s.internal(w, r)
		return
	}
	if err != nil {
		s.paneSnapshotError(w, r, err)
		return
	}
	s.json(w, 200, struct {
		PaneID     string `json:"paneId"`
		CapturedAt string `json:"capturedAt"`
		tmux.PaneSnapshot
	}{PaneID: paneID, CapturedAt: capturedAt, PaneSnapshot: snapshot})
}

func (s *Server) paneSnapshotError(w http.ResponseWriter, r *http.Request, err error) {
	code := sshconnector.Code(err)
	switch code {
	case "conflict":
		s.fail(w, r, 409, code, "Pane atau server tmux berubah. Periksa inventaris kembali.")
	case "not_found":
		s.fail(w, r, 404, code, "Pane tidak lagi ditemukan. Periksa inventaris kembali.")
	case "access_revoked":
		s.fail(w, r, 403, code, "Akses SSH berubah. Isi pane tidak ditampilkan.")
	case "tmux_unsupported":
		s.fail(w, r, 422, code, "Snapshot pane memerlukan tmux 3.8.")
	case "output_limit", "pane_snapshot_limit":
		s.fail(w, r, 503, code, "Snapshot pane melebihi batas ukuran.")
	case "pane_snapshot_invalid", "command_failed":
		s.fail(w, r, 503, code, "Snapshot pane belum dapat dibaca.")
	default:
		s.observationError(w, r, err)
	}
}
