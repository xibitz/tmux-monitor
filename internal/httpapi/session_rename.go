package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type sessionRenamer interface {
	RenameSession(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.RenameRequest, func(context.Context) error) error
}

var operationKey = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *Server) sessionOptions(w http.ResponseWriter, r *http.Request) {
	u := user(r)
	var found string
	err := s.db.QueryRowContext(r.Context(), `SELECT id FROM hosts WHERE id=? AND transport='ssh' AND (? OR id IN (SELECT host_id FROM host_access WHERE user_id=? AND action='tmux:read'))`, r.PathValue("id"), u.IsAdmin, u.ID).Scan(&found)
	if err != nil {
		s.fail(w, r, 404, "not_found", "Host tidak ditemukan atau tidak diizinkan.")
		return
	}
	_, _, err = s.renameAccess(r.Context(), u, found)
	renameAllowed := err == nil
	_, _, killErr := s.lifecycleAccess(r.Context(), u, found, "tmux:kill")
	lifecycle := s.cfg.MutationsEnabled && s.cfg.SessionLifecycleEnabled
	windowWrite := s.windowMutationsEnabled() && renameAllowed
	s.json(w, 200, map[string]bool{
		"renameEnabled":    s.cfg.MutationsEnabled,
		"canRename":        s.cfg.MutationsEnabled && renameAllowed,
		"lifecycleEnabled": lifecycle,
		"canCreate":        lifecycle && renameAllowed,
		"canKill":          lifecycle && killErr == nil,
		"canRenameWindow":  windowWrite,
		"canCreateWindow":  windowWrite,
	})
}

func (s *Server) renameAccess(ctx context.Context, u identity.User, id string) (Host, sshconnector.Record, error) {
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM hosts WHERE id=? AND transport='ssh' AND (? OR (SELECT count(*) FROM host_access WHERE host_id=hosts.id AND user_id=? AND action IN ('tmux:read','tmux:write'))=2)`, id, u.IsAdmin, u.ID).Scan(&found)
	if err != nil {
		return Host{}, sshconnector.Record{}, err
	}
	return s.terminalAccess(ctx, u, id)
}

func (s *Server) renameSession(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.MutationsEnabled {
		s.fail(w, r, 503, "mutations_disabled", "Pengelolaan session belum diaktifkan oleh operator hub.")
		return
	}
	var body tmux.RenameRequest
	if !s.decode(w, r, &body) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	sid := r.PathValue("sid")
	if !operationKey.MatchString(key) || !tmux.ValidRename(sid, body) {
		s.fail(w, r, 422, "invalid_request", "Nama, target, konfirmasi, revision, atau kunci operasi tidak valid.")
		return
	}
	h, record, err := s.renameAccess(r.Context(), user(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, 404, "not_found", "Host SSH tidak tersedia atau tidak diizinkan.")
		return
	}
	raw, _ := json.Marshal([]any{h.ID, sid, record.KeyRef, body})
	requestHash := identity.TokenHash(string(raw))
	keyHash := identity.TokenHash(key)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	var op, previous, state, code string
	var status int
	err = tx.QueryRowContext(r.Context(), `SELECT operation_id,request_hash,state,result_code,status_code FROM tmux_operations WHERE actor_id=? AND key_hash=?`, user(r).ID, keyHash).Scan(&op, &previous, &state, &code, &status)
	if err == nil {
		if previous != requestHash {
			s.renameResponse(w, r, 409, "idempotency_conflict", op, sid)
			return
		}
		if state != "done" {
			s.renameResponse(w, r, 409, "operation_uncertain", op, sid)
			return
		}
		s.renameResponse(w, r, status, code, op, sid)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.internal(w, r)
		return
	}
	var count int
	if err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM tmux_operations`).Scan(&count); err != nil {
		s.internal(w, r)
		return
	}
	if count >= 10000 {
		s.renameResponse(w, r, 429, "operation_limit", "", sid)
		return
	}
	op = storage.ID("op_")
	now := storage.Now()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO tmux_operations(actor_id,key_hash,request_hash,operation_id,host_id,state,created_at,updated_at) VALUES (?,?,?,?,?,'pending',?,?)`, user(r).ID, keyHash, requestHash, op, h.ID, now, now)
	if err == nil {
		err = renameAudit(r, tx, h.ID, body.Epoch, sid, "intent")
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM inventory_cache WHERE host_id=?`, h.ID)
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	if err = tx.Commit(); err != nil {
		s.internal(w, r)
		return
	}
	authorize := func(ctx context.Context) error {
		u, err := identity.Authenticate(ctx, s.db, token(r))
		if err != nil {
			return err
		}
		current, currentRecord, err := s.renameAccess(ctx, u, h.ID)
		if err != nil {
			return err
		}
		if currentRecord.KeyRef != record.KeyRef || current.SocketName != h.SocketName || current.OSUser != h.OSUser || current.TailnetAddress != h.TailnetAddress {
			return identity.ErrCredentials
		}
		return nil
	}
	if s.mutations == nil {
		err = &sshconnector.Failure{Code: "tmux_unsupported"}
	} else {
		err = s.mutations.RenameSession(r.Context(), sshconnector.Host{ID: h.ID, Address: h.TailnetAddress, User: h.OSUser, Socket: h.SocketName}, record, sid, body, authorize)
	}
	status, code = 200, "success"
	if err != nil {
		code = sshconnector.Code(err)
		status = renameStatus(code)
	}
	// Complete result+audit together independently of a disconnected HTTP caller.
	// If persistence fails, the durable pending reservation forbids any retry.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done, err := s.db.BeginTx(ctx, nil)
	if err == nil {
		defer done.Rollback()
		_, err = done.ExecContext(ctx, `UPDATE tmux_operations SET state='done',status_code=?,result_code=?,updated_at=? WHERE operation_id=?`, status, code, storage.Now(), op)
		if err == nil {
			_, err = done.ExecContext(ctx, `INSERT INTO audit_events(actor,action,host_id,target,result,request_id,at) VALUES (?,?,?,?,?,?,?)`, user(r).ID, "tmux.session.rename", h.ID, body.Epoch+":"+sid, code, requestID(r), storage.Now())
		}
		if err == nil {
			err = done.Commit()
		}
	}
	if err != nil {
		status, code = 409, "operation_uncertain"
	}
	s.renameResponse(w, r, status, code, op, sid)
}

func renameAudit(r *http.Request, tx *sql.Tx, host, epoch, sid, result string) error {
	_, err := tx.ExecContext(r.Context(), `INSERT INTO audit_events(actor,action,host_id,target,result,request_id,at) VALUES (?,?,?,?,?,?,?)`, user(r).ID, "tmux.session.rename", host, epoch+":"+sid, result, requestID(r), storage.Now())
	return err
}
func renameStatus(code string) int {
	switch code {
	case "conflict", "duplicate_name", "operation_uncertain":
		return 409
	case "not_found":
		return 404
	case "invalid_request", "tmux_unsupported":
		return 422
	case "busy":
		return 429
	case "access_revoked":
		return 403
	default:
		return 423
	}
}
func (s *Server) renameResponse(w http.ResponseWriter, r *http.Request, status int, code, op, sid string) {
	if op != "" {
		w.Header().Set("X-Operation-ID", op)
	}
	if status == 200 {
		s.json(w, 200, map[string]string{"operationId": op, "state": "succeeded", "sessionId": sid})
		return
	}
	messages := map[string]string{
		"conflict":             "Session atau server tmux berubah. Periksa inventaris sebelum mencoba lagi.",
		"duplicate_name":       "Nama session sudah digunakan. Periksa inventaris lalu pilih nama lain.",
		"not_found":            "Session tidak lagi ditemukan. Periksa inventaris terbaru.",
		"operation_uncertain":  "Hasil operasi belum pasti atau masih diproses. Operasi tidak dikirim ulang. Periksa inventaris sebelum membuat permintaan baru.",
		"idempotency_conflict": "Kunci operasi sudah digunakan untuk permintaan berbeda. Periksa hasil operasi sebelumnya.",
		"operation_limit":      "Batas catatan operasi tercapai. Hub menolak operasi baru; hubungi admin.",
		"tmux_unsupported":     "Ubah nama memerlukan tmux 3.8 yang telah diuji.",
		"access_revoked":       "Izin atau konfigurasi SSH berubah. Operasi dibatalkan sebelum dikirim.",
		"busy":                 "Host sedang diperiksa atau diubah. Periksa inventaris sebelum mencoba lagi.",
	}
	message := messages[code]
	if message == "" {
		message = "Koneksi SSH belum dapat memproses operasi. Periksa inventaris dan konfigurasi host sebelum mencoba lagi."
	}
	s.fail(w, r, status, code, message)
}
