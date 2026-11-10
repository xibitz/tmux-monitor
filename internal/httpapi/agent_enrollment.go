package httpapi

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

func (s *Server) agentEnabled() bool {
	return s.cfg.AgentEnabled && !s.cfg.Development
}

func (s *Server) agentMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	var id string
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(a.id,'') FROM hosts h
		LEFT JOIN agent_identities a ON a.host_id=h.id AND a.revoked_at IS NULL
		WHERE h.id=? AND h.transport='agent'`, r.PathValue("id")).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "Profil Agent tidak ditemukan.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	s.json(w, 200, struct {
		Enabled bool   `json:"enabled"`
		AgentID string `json:"agentId,omitempty"`
	}{s.agentEnabled(), id})
}

func (s *Server) issueAgentEnrollment(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if !s.agentEnabled() {
		s.fail(w, r, 503, "agent_disabled", "Aktifkan Agent pada hub HTTPS.")
		return
	}
	var body struct {
		HostID    string `json:"hostId"`
		TailnetIP string `json:"tailnetIp"`
	}
	if !s.decode(w, r, &body) {
		return
	}
	ip, valid := agentproto.TailnetIP(body.TailnetIP)
	if !valid || !agentproto.ValidHostID(body.HostID) {
		s.fail(w, r, 422, "invalid_request", "ID profil atau alamat IP Tailscale tidak valid.")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	var active bool
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM agent_identities a WHERE a.host_id=h.id AND a.revoked_at IS NULL)
		FROM hosts h WHERE h.id=? AND h.transport='agent'`, body.HostID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "Profil Agent tidak ditemukan.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	if active {
		s.fail(w, r, 409, "agent_already_enrolled", "Cabut identitas Agent aktif sebelum memasangkan ulang.")
		return
	}
	token := storage.ID("") + storage.ID("")
	expires := time.Now().UTC().Truncate(time.Second).Add(10 * time.Minute)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO enrollment_tokens(host_id,token_hash,tailnet_ip,expires_at) VALUES(?,?,?,?)
		ON CONFLICT(host_id) DO UPDATE SET token_hash=excluded.token_hash,tailnet_ip=excluded.tailnet_ip,expires_at=excluded.expires_at`,
		body.HostID, identity.TokenHash(token), ip, storage.Timestamp(expires))
	if err == nil {
		err = auditTx(r, tx, user(r).ID, "agent.enrollment.issue", body.HostID, "success")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	s.json(w, 201, map[string]string{"token": token, "expiresAt": expires.Format(time.RFC3339)})
}

// Only machine endpoints accept a bearer, and only via the trusted loopback
// Serve proxy. Serve replaces the source-IP header on a Tailnet request.
func (s *Server) agentBearer(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if !s.agentEnabled() {
		s.fail(w, r, 503, "agent_disabled", "Agent belum diaktifkan pada hub HTTPS.")
		return "", "", false
	}
	origin, err := url.Parse(s.cfg.Origin)
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	forwarded := r.Header.Values("X-Forwarded-For")
	proto := r.Header.Values("X-Forwarded-Proto")
	host := r.Header.Values("X-Forwarded-Host")
	if err != nil || origin.Scheme != "https" || r.Host != origin.Host || !net.ParseIP(remote).IsLoopback() ||
		len(proto) != 1 || proto[0] != "https" || len(host) != 1 || host[0] != origin.Host || len(forwarded) != 1 ||
		len(r.Header.Values("Tailscale-Funnel-Request")) != 0 || len(r.Header.Values("Origin")) != 0 ||
		len(r.Header.Values("Cookie")) != 0 || r.URL.RawQuery != "" || r.URL.ForceQuery {
		s.fail(w, r, 403, "forbidden", "Koneksi Agent memerlukan Tailscale Serve HTTPS.")
		return "", "", false
	}
	ip, valid := agentproto.TailnetIP(forwarded[0])
	if !valid {
		s.fail(w, r, 403, "forbidden", "IP sumber Agent tidak berasal dari Tailnet.")
		return "", "", false
	}
	auth := r.Header.Values("Authorization")
	if len(auth) == 1 {
		secret, bearer := strings.CutPrefix(auth[0], "Bearer ")
		if bearer && agentproto.ValidSecret(secret) {
			return identity.TokenHash(secret), ip, true
		}
	}
	s.fail(w, r, 401, "unauthenticated", "Credential Agent tidak valid.")
	return "", "", false
}

func (s *Server) enrollAgent(w http.ResponseWriter, r *http.Request) {
	hash, ip, ok := s.agentBearer(w, r)
	if !ok {
		return
	}
	if !s.agents.allowEnrollment(time.Now()) {
		s.fail(w, r, 429, "rate_limited", "Terlalu banyak permintaan pairing. Tunggu lalu coba lagi.")
		return
	}
	var body agentproto.EnrollRequest
	if !s.decode(w, r, &body) {
		return
	}
	if !agentproto.ValidHostID(body.HostID) || !agentproto.ValidName(body.OSUser) || !agentproto.ValidName(body.Socket) {
		s.fail(w, r, 422, "invalid_request", "Profil, akun OS, atau socket Agent tidak valid.")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `DELETE FROM enrollment_tokens
		WHERE host_id=? AND token_hash=? AND tailnet_ip=? AND expires_at>?
		AND EXISTS(SELECT 1 FROM hosts WHERE id=? AND transport='agent' AND os_user=? AND socket_name=?)
		AND NOT EXISTS(SELECT 1 FROM agent_identities WHERE host_id=? AND revoked_at IS NULL)`,
		body.HostID, hash, ip, storage.Now(), body.HostID, body.OSUser, body.Socket, body.HostID)
	if err != nil {
		s.internal(w, r)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		s.internal(w, r)
		return
	}
	if count != 1 {
		s.fail(w, r, 401, "unauthenticated", "Token pairing tidak valid, kedaluwarsa, atau sudah dipakai.")
		return
	}
	id, secret := storage.ID("a_"), storage.ID("")+storage.ID("")
	_, err = tx.ExecContext(r.Context(), `INSERT INTO agent_identities(id,host_id,credential_hash,tailnet_ip,created_at) VALUES(?,?,?,?,?)`,
		id, body.HostID, identity.TokenHash(secret), ip, storage.Now())
	if err == nil {
		err = auditTx(r, tx, id, "agent.enrollment.redeem", body.HostID, "success")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	s.json(w, 201, agentproto.EnrollResult{AgentID: id, Credential: secret})
}

func (s *Server) revokeAgent(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	id := r.PathValue("agentId")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	var hostID string
	err = tx.QueryRowContext(r.Context(), `UPDATE agent_identities SET revoked_at=? WHERE id=? AND revoked_at IS NULL RETURNING host_id`, storage.Now(), id).Scan(&hostID)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "Identitas Agent aktif tidak ditemukan.")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM inventory_cache WHERE host_id=?", hostID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM host_observations WHERE host_id=?", hostID)
	}
	if err == nil {
		err = auditTx(r, tx, user(r).ID, "agent.revoke", hostID, "success")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	s.agents.disconnect(hostID, id)
	w.WriteHeader(204)
}
