package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type sessionLifecycle interface {
	CreateSession(context.Context, sshconnector.Host, sshconnector.Record, tmux.CreateSessionRequest, func(context.Context) error) (string, error)
	KillSession(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.KillSessionRequest, func(context.Context) error) (string, error)
}
type lifecycleExecute func(context.Context, sshconnector.Host, sshconnector.Record, func(context.Context) error) (string, error)

func (s *Server) lifecycleAccess(ctx context.Context, u identity.User, id, action string) (Host, sshconnector.Record, error) {
	if action != "tmux:write" && action != "tmux:kill" {
		return Host{}, sshconnector.Record{}, identity.ErrCredentials
	}
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM hosts WHERE id=? AND transport='ssh' AND (? OR (SELECT count(*) FROM host_access WHERE host_id=hosts.id AND user_id=? AND (action='tmux:read' OR action=?))=2)`, id, u.IsAdmin, u.ID, action).Scan(&found)
	if err != nil {
		return Host{}, sshconnector.Record{}, err
	}
	return s.terminalAccess(ctx, u, found)
}
func (s *Server) lifecycleEnabled(w http.ResponseWriter, r *http.Request) bool {
	if !s.cfg.MutationsEnabled || !s.cfg.SessionLifecycleEnabled {
		s.fail(w, r, 503, "session_lifecycle_disabled", "Create/kill session belum diaktifkan oleh operator hub.")
		return false
	}
	return true
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if !s.lifecycleEnabled(w, r) {
		return
	}
	var body tmux.CreateSessionRequest
	if !s.decode(w, r, &body) {
		return
	}
	if !operationKey.MatchString(r.Header.Get("Idempotency-Key")) || !tmux.ValidCreateSession(body) {
		s.fail(w, r, 422, "invalid_request", "Nama, epoch, revision, konfirmasi, atau kunci operasi tidak valid.")
		return
	}
	execute := func(ctx context.Context, h sshconnector.Host, record sshconnector.Record, authorize func(context.Context) error) (string, error) {
		if s.lifecycle == nil {
			return "", &sshconnector.Failure{Code: "tmux_unsupported"}
		}
		return s.lifecycle.CreateSession(ctx, h, record, body, authorize)
	}
	s.executeLifecycleRequest(w, r, "tmux.session.create", "tmux:write", body.Epoch, "new", body, 201, execute)
}
func (s *Server) killSession(w http.ResponseWriter, r *http.Request) {
	if !s.lifecycleEnabled(w, r) {
		return
	}
	var body tmux.KillSessionRequest
	if !s.decode(w, r, &body) {
		return
	}
	sid := r.PathValue("sid")
	if !operationKey.MatchString(r.Header.Get("Idempotency-Key")) || !tmux.ValidKillSession(sid, body) {
		s.fail(w, r, 422, "invalid_request", "Target, epoch, revision, atau konfirmasi ID session tidak valid.")
		return
	}
	execute := func(ctx context.Context, h sshconnector.Host, record sshconnector.Record, authorize func(context.Context) error) (string, error) {
		if s.lifecycle == nil {
			return "", &sshconnector.Failure{Code: "tmux_unsupported"}
		}
		return s.lifecycle.KillSession(ctx, h, record, sid, body, authorize)
	}
	s.executeLifecycleRequest(w, r, "tmux.session.kill", "tmux:kill", body.Epoch, sid, body, 200, execute)
}

// Reserve the operation before dispatch and persist its acknowledged result.
func (s *Server) executeLifecycleRequest(w http.ResponseWriter, r *http.Request, action, permission, epoch, target string, body any, successStatus int, execute lifecycleExecute) {
	windowOperation := action == windowRenameAction || action == windowCreateAction
	validResult := func(result string) bool {
		if windowOperation {
			return tmux.ValidWindowID(result) && (action != windowRenameAction || result == target)
		}
		return tmux.ValidSessionID(result)
	}
	respond := func(status int, code, op, result string) {
		if code == "success" && !validResult(result) {
			status, code, result = 409, "operation_uncertain", ""
		}
		s.lifecycleResponse(w, r, status, code, op, result, windowOperation)
	}
	h, record, err := s.lifecycleAccess(r.Context(), user(r), r.PathValue("id"), permission)
	if err != nil {
		s.fail(w, r, 404, "not_found", "Host SSH tidak tersedia atau tidak diizinkan.")
		return
	}
	raw, _ := json.Marshal([]any{action, h.ID, target, record.KeyRef, body})
	hash := identity.TokenHash(string(raw))
	keyHash := identity.TokenHash(r.Header.Get("Idempotency-Key"))
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	var op, previous, state, code, resultTarget string
	var status int
	err = tx.QueryRowContext(r.Context(), `SELECT operation_id,request_hash,state,result_code,status_code,result_target FROM tmux_operations WHERE actor_id=? AND key_hash=?`, user(r).ID, keyHash).Scan(&op, &previous, &state, &code, &status, &resultTarget)
	if err == nil {
		if previous != hash {
			respond(409, "idempotency_conflict", op, "")
			return
		}
		if state != "done" {
			respond(409, "operation_uncertain", op, "")
			return
		}
		respond(status, code, op, resultTarget)
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
		respond(429, "operation_limit", "", "")
		return
	}
	op = storage.ID("op_")
	now := storage.Now()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO tmux_operations(actor_id,key_hash,request_hash,operation_id,host_id,state,created_at,updated_at) VALUES (?,?,?,?,?,'pending',?,?)`, user(r).ID, keyHash, hash, op, h.ID, now, now)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events(actor,action,host_id,target,result,request_id,at) VALUES (?,?,?,?,?,?,?)`, user(r).ID, action, h.ID, epoch+":"+target, "intent", requestID(r), now)
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
		current, currentRecord, err := s.lifecycleAccess(ctx, u, h.ID, permission)
		if err != nil {
			return err
		}
		if currentRecord.KeyRef != record.KeyRef || current.SocketName != h.SocketName || current.OSUser != h.OSUser || current.TailnetAddress != h.TailnetAddress {
			return identity.ErrCredentials
		}
		return nil
	}
	resultTarget, err = execute(r.Context(), sshconnector.Host{ID: h.ID, Address: h.TailnetAddress, User: h.OSUser, Socket: h.SocketName}, record, authorize)
	status, code = successStatus, "success"
	if err != nil {
		code = sshconnector.Code(err)
		status = renameStatus(code)
		resultTarget = ""
		if code == "mutation_limit" {
			status = 422
		}
		if code == "window_linked" {
			status = 409
		}
	}
	if err == nil && !validResult(resultTarget) {
		status, code, resultTarget = 409, "operation_uncertain", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done, err := s.db.BeginTx(ctx, nil)
	if err == nil {
		defer done.Rollback()
		_, err = done.ExecContext(ctx, `UPDATE tmux_operations SET state='done',status_code=?,result_code=?,result_target=?,updated_at=? WHERE operation_id=?`, status, code, resultTarget, storage.Now(), op)
		auditTarget := target
		if resultTarget != "" {
			auditTarget = resultTarget
		}
		if err == nil {
			_, err = done.ExecContext(ctx, `INSERT INTO audit_events(actor,action,host_id,target,result,request_id,at) VALUES (?,?,?,?,?,?,?)`, user(r).ID, action, h.ID, epoch+":"+auditTarget, code, requestID(r), storage.Now())
		}
		if err == nil {
			err = done.Commit()
		}
	}
	if err != nil {
		status, code, resultTarget = 409, "operation_uncertain", ""
	}
	respond(status, code, op, resultTarget)
}

func (s *Server) lifecycleResponse(w http.ResponseWriter, r *http.Request, status int, code, op, target string, windowOperation bool) {
	if op != "" {
		w.Header().Set("X-Operation-ID", op)
	}
	if code == "success" && (status == 200 || status == 201) {
		field := "sessionId"
		if windowOperation {
			field = "windowId"
		}
		s.json(w, status, map[string]string{"operationId": op, "state": "succeeded", field: target})
		return
	}
	if windowOperation {
		switch code {
		case "window_linked":
			s.fail(w, r, status, code, "Window tertaut ke beberapa tempat. Ubah nama tersedia untuk window dengan satu tautan.")
			return
		case "duplicate_name":
			s.fail(w, r, status, code, "Nama window sudah digunakan dalam session ini.")
			return
		case "conflict", "not_found":
			s.fail(w, r, status, code, "Window atau session berubah. Periksa inventaris kembali.")
			return
		}
	}
	messages := map[string]string{
		"server_required":  "Belum ada server tmux aktif pada socket ini. Buat session awal melalui administrasi host, lalu periksa inventaris kembali.",
		"mutation_limit":   "Struktur session melebihi batas operasi ini. Periksa host sebelum membuat permintaan baru.",
		"tmux_unsupported": "Pengelolaan session ini memerlukan tmux 3.8.",
	}
	if message := messages[code]; message != "" {
		s.fail(w, r, status, code, message)
		return
	}
	s.renameResponse(w, r, status, code, op, target)
}
