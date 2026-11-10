package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xibitz/tmux-monitor/internal/config"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

type contextKey int

const (
	requestKey contextKey = iota
	userKey
	tokenKey
	nonceKey
)

type Server struct {
	db                   *sql.DB
	cfg                  config.Config
	assets               fs.FS
	log                  *slog.Logger
	router               http.Handler
	loginMu              sync.Mutex
	loginWindow          time.Time
	loginAttempts        int
	ssh                  sshObserver
	terminal             terminalOpener
	paneSnapshots        paneCapturer
	mutations            sessionRenamer
	lifecycle            sessionLifecycle
	windowMutations      windowMutator
	layoutMutations      layoutMutator
	agents               agentConnections
	viewMu               sync.Mutex
	tickets              map[string]terminalTicket
	viewers              map[string]*terminalViewer
	terminalInterval     time.Duration
	terminalPingInterval time.Duration
	terminalPingTimeout  time.Duration
	terminalRoot         context.Context
	stopTerminals        context.CancelFunc
	terminalWG           sync.WaitGroup
	terminalClosing      bool
}

func New(db *sql.DB, cfg config.Config, assets fs.FS, logger *slog.Logger) *Server {
	s := &Server{db: db, cfg: cfg, assets: assets, log: logger, tickets: map[string]terminalTicket{}, viewers: map[string]*terminalViewer{}, terminalInterval: 2 * time.Second, terminalPingInterval: 5 * time.Second, terminalPingTimeout: 5 * time.Second}
	s.terminalRoot, s.stopTerminals = context.WithCancel(context.Background())
	s.agents = agentConnections{peers: make(map[string]*agentPeer), handshakes: make(chan struct{}, 8), snapshots: make(chan struct{}, 4)}
	manager := sshconnector.New(s.credentials(), cfg.AllowedSSHHosts)
	s.ssh = manager
	s.terminal = manager
	s.paneSnapshots = manager
	s.mutations = manager
	s.lifecycle = manager
	s.windowMutations = manager
	s.layoutMutations = manager
	api := http.NewServeMux()
	api.HandleFunc("POST /api/v1/auth/login", s.login)
	api.HandleFunc("POST /api/v1/auth/logout", s.logout)
	api.HandleFunc("GET /api/v1/me", s.me)
	api.HandleFunc("GET /api/v1/hosts", s.listHosts)
	api.HandleFunc("GET /api/v1/hosts/options", s.hostOptions)
	api.HandleFunc("GET /api/v1/hosts/{id}/ssh", s.sshMetadata)
	api.HandleFunc("GET /api/v1/hosts/{id}/agent", s.agentMetadata)
	api.HandleFunc("POST /api/v1/agents/enrollments", s.issueAgentEnrollment)
	api.HandleFunc("POST /api/v1/agents/{agentId}/revoke", s.revokeAgent)
	api.HandleFunc("POST /api/v1/hosts/{id}/probe", s.probe)
	api.HandleFunc("POST /api/v1/hosts", s.createHost)
	api.HandleFunc("PATCH /api/v1/hosts/{id}", s.updateHost)
	api.HandleFunc("DELETE /api/v1/hosts/{id}", s.deleteHost)
	api.HandleFunc("GET /api/v1/hosts/{id}/inventory", s.inventory)
	api.HandleFunc("POST /api/v1/hosts/{id}/panes/{pid}/snapshot", s.capturePane)
	api.HandleFunc("POST /api/v1/hosts/{id}/panes/{pid}/split", s.splitPane)
	api.HandleFunc("DELETE /api/v1/hosts/{id}/panes/{pid}", s.killPane)
	api.HandleFunc("GET /api/v1/hosts/{id}/session-options", s.sessionOptions)
	api.HandleFunc("PATCH /api/v1/hosts/{id}/sessions/{sid}", s.renameSession)
	api.HandleFunc("POST /api/v1/hosts/{id}/sessions", s.createSession)
	api.HandleFunc("DELETE /api/v1/hosts/{id}/sessions/{sid}", s.killSession)
	api.HandleFunc("POST /api/v1/hosts/{id}/sessions/{sid}/windows", s.createWindow)
	api.HandleFunc("PATCH /api/v1/hosts/{id}/windows/{wid}", s.renameWindow)
	api.HandleFunc("DELETE /api/v1/hosts/{id}/windows/{wid}", s.killWindow)
	api.HandleFunc("GET /api/v1/audit", s.listAudit)
	api.HandleFunc("GET /api/v1/terminal/options", s.terminalOptions)
	api.HandleFunc("POST /api/v1/terminal/tickets", s.terminalTicket)
	api.HandleFunc("POST /api/v1/terminal/viewers/{viewer}/mode-tickets", s.terminalModeTicket)
	api.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.fail(w, r, 404, "not_found", "Endpoint tidak ditemukan.")
	})
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", s.health)
	root.HandleFunc("POST /api/v1/agents/enroll", s.enrollAgent)
	root.Handle("/api/", s.authorize(api))
	root.HandleFunc("GET /ws/v1/agent", s.agentUpgrade)
	root.Handle("GET /ws/v1/terminal", s.authorize(http.HandlerFunc(s.terminalUpgrade)))
	root.Handle("/ws/", s.authorize(api))
	root.HandleFunc("/", s.static)
	s.router = root
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := storage.ID("req_")
	r = r.WithContext(context.WithValue(r.Context(), requestKey, id))
	nonce := storage.ID("")
	r = r.WithContext(context.WithValue(r.Context(), nonceKey, nonce))
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	csp := "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; font-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'"
	if s.cfg.Development {
		csp = strings.Replace(csp, "style-src 'self'", "style-src 'self' 'unsafe-inline'", 1)
		csp = strings.Replace(csp, "connect-src 'self'", "connect-src 'self' "+strings.Replace(s.cfg.Origin, "http://", "ws://", 1), 1)
	} else {
		csp = strings.Replace(csp, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1)
		csp = strings.Replace(csp, "connect-src 'self'", "connect-src 'self' "+strings.Replace(s.cfg.Origin, "https://", "wss://", 1), 1)
	}
	w.Header().Set("Content-Security-Policy", csp)
	if !s.cfg.Development {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	w.Header().Set("Cache-Control", "no-store")
	defer func() {
		if recover() != nil {
			s.log.Error("request panic", "request_id", id)
			s.fail(w, r, 500, "internal_error", "Terjadi kesalahan internal.")
		}
	}()
	// Avoid exposing the localhost listener through an untrusted Host header.
	origin, _ := url.Parse(s.cfg.Origin)
	if r.Host != origin.Host && r.Host != s.cfg.Listen {
		s.fail(w, r, 403, "forbidden", "Host tidak diizinkan.")
		return
	}
	s.router.ServeHTTP(w, r)
}

func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" && r.Method == http.MethodPost {
			if !s.originOK(r) {
				s.fail(w, r, 403, "forbidden", "Origin tidak diizinkan.")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(identity.CookieName)
		if err != nil {
			s.fail(w, r, 401, "unauthenticated", "Silakan masuk terlebih dahulu.")
			return
		}
		user, err := identity.Authenticate(r.Context(), s.db, cookie.Value)
		if errors.Is(err, identity.ErrCredentials) {
			s.fail(w, r, 401, "unauthenticated", "Sesi tidak berlaku. Silakan masuk kembali.")
			return
		}
		if err != nil {
			s.internal(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead || strings.HasPrefix(r.URL.Path, "/ws/") {
			if !s.originOK(r) {
				s.fail(w, r, 403, "forbidden", "Origin tidak diizinkan.")
				return
			}
			if !(r.Method == http.MethodGet && r.URL.Path == "/ws/v1/terminal") && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(identity.CSRF(cookie.Value))) != 1 {
				s.fail(w, r, 403, "forbidden", "Token CSRF tidak berlaku.")
				return
			}
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		ctx = context.WithValue(ctx, tokenKey, cookie.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) originOK(r *http.Request) bool { return r.Header.Get("Origin") == s.cfg.Origin }
func user(r *http.Request) identity.User        { return r.Context().Value(userKey).(identity.User) }
func token(r *http.Request) string              { return r.Context().Value(tokenKey).(string) }
func requestID(r *http.Request) string          { return r.Context().Value(requestKey).(string) }

func (s *Server) json(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	s.json(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "requestId": requestID(r)}})
}
func (s *Server) internal(w http.ResponseWriter, r *http.Request) {
	s.log.Error("request failed", "request_id", requestID(r))
	s.fail(w, r, 500, "internal_error", "Terjadi kesalahan internal.")
}
func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		s.fail(w, r, 400, "invalid_request", "Gunakan Content-Type application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		s.fail(w, r, 400, "invalid_request", "Body JSON tidak valid atau terlalu besar.")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		s.fail(w, r, 400, "invalid_request", "Body harus berisi satu objek JSON.")
		return false
	}
	return true
}

func (s *Server) admin(w http.ResponseWriter, r *http.Request) bool {
	if user(r).IsAdmin {
		return true
	}
	if err := s.audit(r, user(r).ID, "access.denied", "", "denied"); err != nil {
		s.internal(w, r)
		return false
	}
	s.fail(w, r, 403, "forbidden", "Akses admin diperlukan.")
	return false
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil || version != storage.LatestVersion {
		s.fail(w, r, 503, "storage_unavailable", "Penyimpanan belum siap.")
		return
	}
	s.json(w, 200, map[string]any{"status": "ok", "schemaVersion": version})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	s.loginMu.Lock()
	if time.Since(s.loginWindow) >= time.Minute {
		s.loginWindow = time.Now()
		s.loginAttempts = 0
	}
	s.loginAttempts++
	allowed := s.loginAttempts <= 5
	s.loginMu.Unlock()
	if !allowed {
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, 429, "rate_limited", "Terlalu banyak percobaan. Coba lagi dalam satu menit.")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !s.decode(w, r, &body) {
		return
	}
	if len(body.Username) > 64 || len(body.Password) > 1024 {
		if err := s.audit(r, "anonymous", "auth.login", "", "failure"); err != nil {
			s.internal(w, r)
			return
		}
		s.fail(w, r, 401, "unauthenticated", "Nama pengguna atau kata sandi salah.")
		return
	}
	u, session, err := identity.Login(r.Context(), s.db, body.Username, body.Password)
	if errors.Is(err, identity.ErrCredentials) {
		if err = s.audit(r, "anonymous", "auth.login", "", "failure"); err != nil {
			s.internal(w, r)
			return
		}
		s.fail(w, r, 401, "unauthenticated", "Nama pengguna atau kata sandi salah.")
		return
	}
	if err != nil {
		s.internal(w, r)
		return
	}
	if err = s.audit(r, u.ID, "auth.login", "", "success"); err != nil {
		_, _ = s.db.ExecContext(r.Context(), "DELETE FROM app_sessions WHERE id_hash=?", identity.TokenHash(session))
		s.internal(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: identity.CookieName, Value: session, Path: "/", HttpOnly: true, Secure: !s.cfg.Development, SameSite: http.SameSiteStrictMode, MaxAge: int(identity.SessionLifetime.Seconds()), Expires: time.Now().Add(identity.SessionLifetime)})
	s.json(w, 200, map[string]any{"user": u, "csrfToken": identity.CSRF(session)})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	s.json(w, 200, map[string]any{"user": user(r), "csrfToken": identity.CSRF(token(r))})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM app_sessions WHERE id_hash=?", identity.TokenHash(token(r))); err != nil {
		s.internal(w, r)
		return
	}
	if err = auditTx(r, tx, user(r).ID, "auth.logout", "", "success"); err != nil {
		s.internal(w, r)
		return
	}
	if err = tx.Commit(); err != nil {
		s.internal(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: identity.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: !s.cfg.Development, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	w.WriteHeader(204)
}

type AuditEvent struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	HostID    string `json:"hostId,omitempty"`
	Result    string `json:"result"`
	RequestID string `json:"requestId"`
	At        string `json:"at"`
	Target    string `json:"target,omitempty"`
}

func (s *Server) audit(r *http.Request, actor, action, host, result string) error {
	_, err := s.db.ExecContext(r.Context(), "INSERT INTO audit_events(actor,action,host_id,result,request_id,at) VALUES (?,?,?,?,?,?)", actor, action, host, result, requestID(r), storage.Now())
	return err
}
func auditTx(r *http.Request, tx *sql.Tx, actor, action, host, result string) error {
	_, err := tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor,action,host_id,result,request_id,at) VALUES (?,?,?,?,?,?)", actor, action, host, result, requestID(r), storage.Now())
	return err
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			s.fail(w, r, 422, "invalid_request", "Limit harus 1–100.")
			return
		}
		limit = n
	}
	before := int64(1<<63 - 1)
	if raw := r.URL.Query().Get("before"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			s.fail(w, r, 422, "invalid_request", "Cursor tidak valid.")
			return
		}
		before = n
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT id,actor,action,host_id,result,request_id,at,target FROM audit_events WHERE id<? ORDER BY id DESC LIMIT ?", before, limit+1)
	if err != nil {
		s.internal(w, r)
		return
	}
	defer rows.Close()
	events := []AuditEvent{}
	for rows.Next() {
		var event AuditEvent
		if err = rows.Scan(&event.ID, &event.Actor, &event.Action, &event.HostID, &event.Result, &event.RequestID, &event.At, &event.Target); err != nil {
			s.internal(w, r)
			return
		}
		events = append(events, event)
	}
	if rows.Err() != nil {
		s.internal(w, r)
		return
	}
	cursor := ""
	if len(events) > limit {
		events = events[:limit]
		cursor = strconv.FormatInt(events[len(events)-1].ID, 10)
	}
	s.json(w, 200, map[string]any{"events": events, "nextCursor": cursor})
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.fail(w, r, 405, "method_not_allowed", "Metode tidak diizinkan.")
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if strings.HasPrefix(name, ".") || strings.Contains(name, "/.") {
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(s.assets, name); err != nil {
		if path.Ext(name) != "" || strings.HasPrefix(name, "api") || strings.HasPrefix(name, "ws") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	if _, err := fs.Stat(s.assets, name); err != nil {
		s.fail(w, r, 503, "web_unavailable", "Antarmuka belum dibangun. Jalankan build web terlebih dahulu.")
		return
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if name == "index.html" {
		body, err := fs.ReadFile(s.assets, name)
		if err != nil {
			s.internal(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body = bytes.ReplaceAll(body, []byte("__CSP_NONCE__"), []byte(r.Context().Value(nonceKey).(string)))
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(body))
		return
	}
	clone := r.Clone(r.Context())
	clone.URL.Path = "/" + name
	http.FileServer(http.FS(s.assets)).ServeHTTP(w, clone)
}
