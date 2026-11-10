package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type sshObserver interface {
	Snapshot(context.Context, sshconnector.Host, sshconnector.Record) (tmux.Inventory, error)
}

func (s *Server) credentials() sshconnector.Store {
	return sshconnector.Store{DB: s.db, DataDir: s.cfg.DataDir}
}

const observedHostColumns = `h.id,h.label,h.transport,h.tailnet_address,h.os_user,h.socket_name,h.environment,h.created_at,h.updated_at,
COALESCE(o.state,'UNREACHABLE'),
CASE WHEN h.transport='agent' THEN COALESCE(o.reason,'agent_offline') WHEN c.host_id IS NULL THEN 'credentials_required' ELSE COALESCE(o.reason,'not_probed') END,
o.observed_at`
const observedHostJoin = ` FROM hosts h LEFT JOIN host_observations o ON o.host_id=h.id LEFT JOIN ssh_credentials c ON c.host_id=h.id `

func scanObservedHost(row scanner) (Host, error) {
	var h Host
	err := row.Scan(&h.ID, &h.Label, &h.Transport, &h.TailnetAddress, &h.OSUser, &h.SocketName, &h.Environment, &h.CreatedAt, &h.UpdatedAt, &h.State, &h.StatusReason, &h.ObservedAt)
	h.Stale = true
	if h.ObservedAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *h.ObservedAt); err == nil {
			h.Stale = time.Since(t) > 30*time.Second || h.State != "ONLINE"
		}
	}
	return h, err
}
func (s *Server) observedHost(ctx context.Context, id string) (Host, error) {
	h, err := scanObservedHost(s.db.QueryRowContext(ctx, "SELECT "+observedHostColumns+observedHostJoin+"WHERE h.id=?", id))
	if err != nil {
		return h, err
	}
	return s.applyAgentStatus(h), nil
}

func (s *Server) sshMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	h, err := s.observedHost(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "Profil host tidak ditemukan.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	if h.Transport != "ssh" {
		s.fail(w, r, 422, "invalid_request", "Profil ini tidak memakai SSH.")
		return
	}
	record, err := s.credentials().Load(r.Context(), h.ID)
	if errors.Is(err, sshconnector.ErrRequired) {
		s.json(w, 200, sshconnector.Metadata{})
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	s.json(w, 200, record.Metadata)
}

func (s *Server) probe(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	h, err := s.observedHost(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "Profil host tidak ditemukan.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	inv, err := s.observe(r, h, true)
	if err != nil {
		s.observationError(w, r, err)
		return
	}
	h, err = s.observedHost(r.Context(), h.ID)
	if err != nil {
		s.internal(w, r)
		return
	}
	s.json(w, 200, map[string]any{"host": h, "inventory": inv})
}

func (s *Server) observe(r *http.Request, h Host, audit bool) (tmux.Inventory, error) {
	if h.Transport == "agent" {
		return s.observeAgent(r, h, audit)
	}
	var inv tmux.Inventory
	if h.Transport != "ssh" {
		return inv, errors.New("unknown host transport")
	}
	record, err := s.credentials().Load(r.Context(), h.ID)
	if err != nil && !errors.Is(err, sshconnector.ErrRequired) {
		return inv, err
	}
	if err == nil {
		inv, err = s.ssh.Snapshot(r.Context(), sshconnector.Host{ID: h.ID, Address: h.TailnetAddress, User: h.OSUser, Socket: h.SocketName}, record)
	}
	code, state, result := "ok", "ONLINE", "success"
	if err == nil && inv.StatusReason != "" {
		code = inv.StatusReason
	}
	if err != nil {
		code = sshconnector.Code(err)
		state = sshconnector.State(code)
		result = code
	}
	if code == "busy" {
		if audit {
			if auditErr := s.audit(r, user(r).ID, "host.probe", h.ID, "busy"); auditErr != nil {
				return inv, auditErr
			}
		}
		return inv, err
	}
	tx, dbErr := s.db.BeginTx(r.Context(), nil)
	if dbErr != nil {
		return inv, dbErr
	}
	defer tx.Rollback()
	// Discard an in-flight result if credentials were rotated/revoked while SSH
	// was running. An old observation must not resurrect invalidated cache data.
	if record.KeyRef != "" {
		var current string
		checkErr := tx.QueryRowContext(r.Context(), "SELECT key_ref FROM ssh_credentials WHERE host_id=?", h.ID).Scan(&current)
		if errors.Is(checkErr, sql.ErrNoRows) || checkErr == nil && current != record.KeyRef {
			if audit {
				if auditErr := auditTx(r, tx, user(r).ID, "host.probe", h.ID, "credentials_required"); auditErr != nil {
					return tmux.Inventory{}, auditErr
				}
				if commitErr := tx.Commit(); commitErr != nil {
					return tmux.Inventory{}, commitErr
				}
			}
			return tmux.Inventory{}, sshconnector.ErrRequired
		}
		if checkErr != nil {
			return tmux.Inventory{}, checkErr
		}
	}
	if dbErr = storeObservation(r.Context(), tx, h.ID, state, code, &inv, err == nil); dbErr != nil {
		return inv, dbErr
	}
	if audit {
		if dbErr = auditTx(r, tx, user(r).ID, "host.probe", h.ID, result); dbErr != nil {
			return inv, dbErr
		}
	}
	if dbErr = tx.Commit(); dbErr != nil {
		return inv, dbErr
	}
	if err != nil {
		return s.cachedObservation(r.Context(), h, state, code, err)
	}
	return inv, nil
}

func storeObservation(ctx context.Context, tx *sql.Tx, hostID, state, code string, inv *tmux.Inventory, cache bool) error {
	now := storage.Now()
	_, err := tx.ExecContext(ctx, `INSERT INTO host_observations(host_id,state,reason,observed_at) VALUES (?,?,?,?)
		ON CONFLICT(host_id) DO UPDATE SET state=excluded.state,reason=excluded.reason,observed_at=excluded.observed_at`,
		hostID, state, code, now)
	if err != nil || !cache {
		return err
	}
	inv.ObservedAt, inv.Stale = now, false
	body, err := json.Marshal(inv)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_cache(host_id,snapshot_json,observed_at) VALUES (?,?,?)
		ON CONFLICT(host_id) DO UPDATE SET snapshot_json=excluded.snapshot_json,observed_at=excluded.observed_at`,
		hostID, string(body), now)
	return err
}

func (s *Server) cachedObservation(ctx context.Context, h Host, state, code string, original error) (tmux.Inventory, error) {
	var body string
	var inv tmux.Inventory
	err := s.db.QueryRowContext(ctx, "SELECT snapshot_json FROM inventory_cache WHERE host_id=? AND length(snapshot_json)<=?", h.ID, 4*tmux.MaxOutput).Scan(&body)
	if err == nil && json.Unmarshal([]byte(body), &inv) == nil && inv.HostID == h.ID && inv.SocketID == h.SocketName &&
		inv.Transport == h.Transport && inv.Sessions != nil && validObservationTime(inv.ObservedAt) {
		inv.Stale, inv.State, inv.StatusReason = true, state, code
		if h.Transport == "agent" {
			inv.Capabilities.PTYAttach = false
		}
		return inv, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return tmux.Inventory{}, err
	}
	return tmux.Inventory{}, original
}

func isObservationFailure(err error) bool {
	var failure *sshconnector.Failure
	return errors.As(err, &failure) || errors.Is(err, sshconnector.ErrRequired) || errors.Is(err, sshconnector.ErrCredentials)
}

func validObservationTime(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func (s *Server) observationError(w http.ResponseWriter, r *http.Request, err error) {
	code := sshconnector.Code(err)
	status, message := 423, "Host belum dapat dijangkau. Session tmux tidak dianggap berhenti."
	switch code {
	case "credentials_required":
		status, message = 422, "Konfigurasi SSH belum tersedia. Pasang key dan host key terverifikasi melalui perintah lokal hub ssh-configure."
	case "credentials_invalid":
		status, message = 422, "Konfigurasi key SSH tidak dapat digunakan. Periksa atau pasang ulang melalui CLI lokal."
	case "address_not_allowed":
		status, message = 422, "Alamat SSH tidak lagi diizinkan dalam konfigurasi hub."
	case "address_not_tailnet":
		status, message = 422, "Alamat SSH harus mengarah hanya ke IP Tailscale."
	case "host_key_mismatch":
		message = "Host key SSH tidak cocok dengan key yang telah diverifikasi. Koneksi dihentikan."
	case "authentication_failed":
		message = "Autentikasi SSH gagal. Periksa akun OS dan public key yang dipasang pada host."
	case "timeout":
		message = "Batas waktu koneksi atau observasi terlampaui."
	case "os_user_mismatch":
		status, message = 503, "Akun OS yang terhubung tidak sesuai dengan profil."
	case "tmux_missing":
		status, message = 503, "tmux yang didukung belum ditemukan pada host."
	case "inventory_invalid", "inventory_changed":
		status, message = 503, "Inventaris tmux belum dapat diverifikasi. Coba periksa ulang."
	case "output_limit":
		status, message = 503, "Output inventaris melebihi batas yang diizinkan."
	case "command_failed":
		status, message = 503, "Perintah observasi tmux gagal. Periksa socket dan izin akun OS."
	case "agent_offline":
		message = "Agent belum terhubung. Jalankan Agent pada host yang dipasangkan."
	case "agent_reconciling":
		status, message = 503, "Agent sedang menyegarkan inventaris. Tunggu lalu periksa lagi."
	case "agent_disabled":
		status, message = 503, "Agent belum diaktifkan pada hub HTTPS."
	case "busy":
		status, message = 429, "Pemeriksaan host sedang berjalan. Tunggu lalu coba lagi."
	default:
		if !isObservationFailure(err) {
			s.internal(w, r)
			return
		}
	}
	s.fail(w, r, status, code, message)
}
