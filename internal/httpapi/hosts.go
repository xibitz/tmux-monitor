package httpapi

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/xibitz/tmux-monitor/internal/storage"
)

type HostInput struct {
	Label          string `json:"label"`
	Transport      string `json:"transport"`
	TailnetAddress string `json:"tailnetAddress"`
	OSUser         string `json:"osUser"`
	SocketName     string `json:"socketName"`
	Environment    string `json:"environment"`
}
type Host struct {
	HostInput
	ID           string  `json:"id"`
	State        string  `json:"state"`
	StatusReason string  `json:"statusReason"`
	ObservedAt   *string `json:"observedAt"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$`)
var dnsName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$`)

func (s *Server) validateHost(in *HostInput) string {
	in.Label = strings.TrimSpace(in.Label)
	in.Environment = strings.TrimSpace(in.Environment)
	in.TailnetAddress = strings.ToLower(strings.TrimSpace(in.TailnetAddress))
	if !utf8.ValidString(in.Label) || utf8.RuneCountInString(in.Label) < 1 || utf8.RuneCountInString(in.Label) > 80 || strings.ContainsAny(in.Label, "\r\n\x00") {
		return "Nama host harus berisi 1–80 karakter."
	}
	if utf8.RuneCountInString(in.Environment) > 40 || strings.ContainsAny(in.Environment, "\r\n\x00") {
		return "Label lingkungan maksimal 40 karakter."
	}
	if !safeName.MatchString(in.OSUser) {
		return "Nama akun OS tidak valid."
	}
	if in.SocketName == "" {
		in.SocketName = "default"
	}
	if !safeName.MatchString(in.SocketName) {
		return "Nama socket tmux tidak valid."
	}
	switch in.Transport {
	case "ssh":
		if len(in.TailnetAddress) > 253 || (!dnsName.MatchString(in.TailnetAddress) && net.ParseIP(in.TailnetAddress) == nil) {
			return "Alamat host SSH tidak valid."
		}
		if !s.cfg.AllowedSSHHosts[in.TailnetAddress] {
			return "Alamat SSH belum diizinkan. Tambahkan alamat ini ke TMUX_MONITOR_ALLOWED_SSH_HOSTS pada konfigurasi hub, lalu mulai ulang hub."
		}
	case "agent":
		if in.TailnetAddress != "" {
			return "Profil Agent tidak menggunakan alamat SSH."
		}
	default:
		return "Transport harus ssh atau agent."
	}
	return ""
}

func (s *Server) hostOptions(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	addresses := []string{}
	for address, allowed := range s.cfg.AllowedSSHHosts {
		if allowed {
			addresses = append(addresses, address)
		}
	}
	sort.Strings(addresses)
	s.json(w, http.StatusOK, map[string]any{"allowedSSHHosts": addresses})
}

const hostColumns = "id,label,transport,tailnet_address,os_user,socket_name,environment,created_at,updated_at"

type scanner interface{ Scan(...any) error }

func scanHost(row scanner) (Host, error) {
	h := Host{State: "UNREACHABLE", StatusReason: "connector_pending"}
	err := row.Scan(&h.ID, &h.Label, &h.Transport, &h.TailnetAddress, &h.OSUser, &h.SocketName, &h.Environment, &h.CreatedAt, &h.UpdatedAt)
	return h, err
}

func (s *Server) listHosts(w http.ResponseWriter, r *http.Request) {
	u := user(r)
	rows, err := s.db.QueryContext(r.Context(), "SELECT "+hostColumns+" FROM hosts WHERE ? OR id IN (SELECT host_id FROM host_access WHERE user_id=? AND action='host:read') ORDER BY label,id", u.IsAdmin, u.ID)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer rows.Close()
	hosts := []Host{}
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			s.internal(w, r)
			return
		}
		hosts = append(hosts, h)
	}
	if rows.Err() != nil {
		s.internal(w, r)
		return
	}
	s.json(w, 200, map[string]any{"hosts": hosts})
}

func (s *Server) createHost(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	var input HostInput
	if !s.decode(w, r, &input) {
		return
	}
	if msg := s.validateHost(&input); msg != "" {
		s.fail(w, r, 422, "invalid_request", msg)
		return
	}
	h := Host{HostInput: input, ID: storage.ID("h_"), State: "UNREACHABLE", StatusReason: "connector_pending", CreatedAt: storage.Now(), UpdatedAt: storage.Now()}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO hosts(id,label,transport,tailnet_address,os_user,socket_name,environment,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, h.ID, h.Label, h.Transport, h.TailnetAddress, h.OSUser, h.SocketName, h.Environment, h.CreatedAt, h.UpdatedAt)
	if err != nil {
		s.internal(w, r)
		return
	}
	if err = auditTx(r, tx, user(r).ID, "host.create", h.ID, "success"); err != nil {
		s.internal(w, r)
		return
	}
	if err = tx.Commit(); err != nil {
		s.internal(w, r)
		return
	}
	w.Header().Set("Location", "/api/v1/hosts/"+h.ID)
	s.json(w, 201, h)
}

func (s *Server) updateHost(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	var input HostInput
	if !s.decode(w, r, &input) {
		return
	}
	if msg := s.validateHost(&input); msg != "" {
		s.fail(w, r, 422, "invalid_request", msg)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	h, err := scanHost(tx.QueryRowContext(r.Context(), "SELECT "+hostColumns+" FROM hosts WHERE id=?", r.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "Profil host tidak ditemukan.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	if h.Transport != input.Transport || h.TailnetAddress != input.TailnetAddress || h.OSUser != input.OSUser || h.SocketName != input.SocketName {
		s.fail(w, r, 422, "invalid_request", "Ubah label atau lingkungan saja. Buat profil baru untuk mengganti transport, alamat, akun, atau socket.")
		return
	}
	h.HostInput = input
	h.UpdatedAt = storage.Now()
	if _, err = tx.ExecContext(r.Context(), "UPDATE hosts SET label=?,environment=?,updated_at=? WHERE id=?", h.Label, h.Environment, h.UpdatedAt, h.ID); err != nil {
		s.internal(w, r)
		return
	}
	if err = auditTx(r, tx, user(r).ID, "host.update", h.ID, "success"); err != nil {
		s.internal(w, r)
		return
	}
	if err = tx.Commit(); err != nil {
		s.internal(w, r)
		return
	}
	s.json(w, 200, h)
}

func (s *Server) deleteHost(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	result, err := tx.ExecContext(r.Context(), "DELETE FROM hosts WHERE id=?", id)
	if err != nil {
		s.internal(w, r)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		s.internal(w, r)
		return
	}
	if count == 0 {
		s.fail(w, r, 404, "not_found", "Profil host tidak ditemukan.")
		return
	}
	if err = auditTx(r, tx, user(r).ID, "host.delete", id, "success"); err != nil {
		s.internal(w, r)
		return
	}
	if err = tx.Commit(); err != nil {
		s.internal(w, r)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	u := user(r)
	var id string
	err := s.db.QueryRowContext(r.Context(), "SELECT id FROM hosts WHERE id=? AND (? OR id IN (SELECT host_id FROM host_access WHERE user_id=? AND action='tmux:read'))", r.PathValue("id"), u.IsAdmin, u.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "Host tidak ditemukan atau tidak diizinkan.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	s.fail(w, r, 503, "transport_unavailable", "Konektor host belum tersedia pada fondasi ini. Status tmux belum diperiksa.")
}
