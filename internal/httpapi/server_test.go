package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/xibitz/tmux-monitor/internal/config"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

const testOrigin = "https://hub.example.ts.net"
const testPassword = "test-only-password-12345"

type harness struct {
	server *Server
	db     *sql.DB
	cookie *http.Cookie
	csrf   string
	logs   *bytes.Buffer
}

func setup(t *testing.T) *harness {
	t.Helper()
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db, storage.LatestVersion); err != nil {
		t.Fatal(err)
	}
	if err := identity.Bootstrap(context.Background(), db, "admin", testPassword); err != nil {
		t.Fatal(err)
	}
	logs := new(bytes.Buffer)
	s := New(db, config.Config{DataDir: t.TempDir(), Listen: "127.0.0.1:8080", Origin: testOrigin, AllowedSSHHosts: map[string]bool{"laptop.example.ts.net": true}}, fstest.MapFS{"index.html": {Data: []byte("<!doctype html><h1>workspace</h1>")}, "assets/main.js": {Data: []byte("console.log('app')")}}, slog.New(slog.NewJSONHandler(logs, nil)))
	h := &harness{server: s, db: db, logs: logs}
	r := h.request("POST", "/api/v1/auth/login", `{"username":"admin","password":"`+testPassword+`"}`, false, true, "")
	if r.Code != 200 {
		t.Fatalf("login %d: %s", r.Code, r.Body)
	}
	var result struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	h.cookie = r.Result().Cookies()[0]
	h.csrf = result.CSRF
	return h
}

func (h *harness) request(method, path, body string, auth, origin bool, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, testOrigin+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if auth {
		r.AddCookie(h.cookie)
	}
	if origin {
		r.Header.Set("Origin", testOrigin)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	h.server.ServeHTTP(w, r)
	return w
}

func TestLoginCookiesAndSessionRevocation(t *testing.T) {
	h := setup(t)
	if !h.cookie.Secure || !h.cookie.HttpOnly || h.cookie.SameSite != http.SameSiteStrictMode || h.cookie.MaxAge != 43200 {
		t.Fatalf("unsafe cookie: %+v", h.cookie)
	}
	var stored string
	if err := h.db.QueryRow("SELECT id_hash FROM app_sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == h.cookie.Value || stored != identity.TokenHash(h.cookie.Value) {
		t.Fatal("raw session token persisted")
	}
	if r := h.request("GET", "/api/v1/me", "", true, false, ""); r.Code != 200 || !strings.Contains(r.Body.String(), h.csrf) {
		t.Fatalf("me: %d %s", r.Code, r.Body)
	}
	if r := h.request("POST", "/api/v1/auth/logout", "", true, true, h.csrf); r.Code != 204 {
		t.Fatalf("logout: %d %s", r.Code, r.Body)
	}
	if r := h.request("GET", "/api/v1/me", "", true, false, ""); r.Code != 401 {
		t.Fatalf("revoked session accepted: %d", r.Code)
	}
	if err := identity.Bootstrap(context.Background(), h.db, "another", testPassword); err == nil {
		t.Fatal("repeat bootstrap accepted")
	}
}

func TestSecurityBoundaries(t *testing.T) {
	h := setup(t)
	for _, path := range []string{"/api/v1/hosts", "/api/v1/audit", "/ws/v1/terminal", "/api/unknown"} {
		if r := h.request("GET", path, "", false, false, ""); r.Code != 401 {
			t.Errorf("%s unauthenticated: %d", path, r.Code)
		}
	}
	body := `{"label":"Laptop","transport":"ssh","tailnetAddress":"laptop.example.ts.net","osUser":"developer","socketName":"default"}`
	for _, tc := range []struct {
		name   string
		origin bool
		csrf   string
	}{{"missing csrf", true, ""}, {"wrong csrf", true, "wrong"}, {"missing origin", false, h.csrf}} {
		t.Run(tc.name, func(t *testing.T) {
			if r := h.request("POST", "/api/v1/hosts", body, true, tc.origin, tc.csrf); r.Code != 403 {
				t.Fatalf("accepted: %d %s", r.Code, r.Body)
			}
		})
	}
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/hosts", strings.NewReader(body))
	r.AddCookie(h.cookie)
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("X-CSRF-Token", h.csrf)
	w := httptest.NewRecorder()
	h.server.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin request accepted")
	}
	r = httptest.NewRequest("GET", "http://evil.test/api/v1/me", nil)
	r.AddCookie(h.cookie)
	w = httptest.NewRecorder()
	h.server.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted Host accepted")
	}
	if r := h.request("POST", "/api/v1/auth/login", `{}`, false, false, ""); r.Code != 403 {
		t.Fatal("login without Origin accepted")
	}
	if _, err := h.db.Exec("UPDATE app_sessions SET expires_at=?", storage.Timestamp(time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if r := h.request("GET", "/api/v1/me", "", true, false, ""); r.Code != 401 {
		t.Fatal("expired session accepted")
	}
}

func TestHostCRUDValidationAuditAndNoFakeInventory(t *testing.T) {
	h := setup(t)
	for _, body := range []string{
		`{"label":"Bad","transport":"ssh","tailnetAddress":"evil.test","osUser":"dev"}`,
		`{"label":"Bad","transport":"agent","osUser":"dev;id"}`,
		`{"label":"Bad","transport":"agent","osUser":"dev","socketName":"-Lbad"}`,
	} {
		if r := h.request("POST", "/api/v1/hosts", body, true, true, h.csrf); r.Code != 422 {
			t.Fatalf("invalid host accepted: %d %s", r.Code, r.Body)
		}
	}
	if r := h.request("POST", "/api/v1/hosts", `{"label":"Bad","transport":"agent","osUser":"dev","privateKey":"SECRET"}`, true, true, h.csrf); r.Code != 400 {
		t.Fatal("unknown credential field accepted")
	}
	r := h.request("POST", "/api/v1/hosts", `{"label":"Laptop","transport":"ssh","tailnetAddress":"laptop.example.ts.net","osUser":"dev"}`, true, true, h.csrf)
	if r.Code != 201 {
		t.Fatalf("create %d %s", r.Code, r.Body)
	}
	var host Host
	if err := json.Unmarshal(r.Body.Bytes(), &host); err != nil {
		t.Fatal(err)
	}
	if host.SocketName != "default" || host.ObservedAt != nil || host.State != "UNREACHABLE" {
		t.Fatalf("invented host status %+v", host)
	}
	if r = h.request("GET", "/api/v1/hosts/"+host.ID+"/inventory", "", true, false, ""); r.Code != 422 || !strings.Contains(r.Body.String(), "credentials_required") {
		t.Fatal("fake inventory returned")
	}
	if r = h.request("GET", "/api/v1/hosts", "", true, false, ""); r.Code != 200 || !strings.Contains(r.Body.String(), host.ID) {
		t.Fatalf("list %d %s", r.Code, r.Body)
	}
	r = h.request("PATCH", "/api/v1/hosts/"+host.ID, `{"label":"Work","transport":"ssh","tailnetAddress":"laptop.example.ts.net","osUser":"dev","environment":"dev"}`, true, true, h.csrf)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"label":"Work"`) {
		t.Fatalf("patch %d %s", r.Code, r.Body)
	}
	if r = h.request("DELETE", "/api/v1/hosts/"+host.ID, "", true, true, h.csrf); r.Code != 204 {
		t.Fatalf("delete %d", r.Code)
	}
	if r = h.request("DELETE", "/api/v1/hosts/"+host.ID, "", true, true, h.csrf); r.Code != 404 {
		t.Fatal("missing host delete accepted")
	}
	r = h.request("GET", "/api/v1/audit", "", true, false, "")
	for _, action := range []string{"host.create", "host.update", "host.delete"} {
		if !strings.Contains(r.Body.String(), action) {
			t.Errorf("missing audit %s", action)
		}
	}
	for _, sensitive := range []string{testPassword, h.cookie.Value, h.csrf, "SECRET", "privateKey"} {
		if strings.Contains(r.Body.String()+h.logs.String(), sensitive) {
			t.Fatal("secret appeared in audit or logs")
		}
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("API cache permitted")
	}
}

func TestDefaultDenyHostPermissions(t *testing.T) {
	h := setup(t)
	if _, err := h.db.Exec("INSERT INTO users(id,username,password_hash,is_admin,created_at) VALUES ('reader','reader',?,0,?)", identity.DummyHash, storage.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec("INSERT INTO hosts(id,label,transport,os_user,socket_name,created_at,updated_at) VALUES ('private','Private','agent','dev','default',?,?)", storage.Now(), storage.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec("UPDATE app_sessions SET user_id='reader'"); err != nil {
		t.Fatal(err)
	}
	if r := h.request("GET", "/api/v1/hosts", "", true, false, ""); r.Code != 200 || strings.Contains(r.Body.String(), "private") {
		t.Fatalf("host leaked: %d %s", r.Code, r.Body)
	}
	if r := h.request("GET", "/api/v1/hosts/private/inventory", "", true, false, ""); r.Code != 404 {
		t.Fatal("inaccessible host leaked")
	}
	for _, route := range []struct{ method, path, body string }{{"GET", "/api/v1/audit", ""}, {"POST", "/api/v1/hosts", `{}`}, {"DELETE", "/api/v1/hosts/private", ""}} {
		if r := h.request(route.method, route.path, route.body, true, true, h.csrf); r.Code != 403 {
			t.Fatalf("non-admin accepted: %d %s", r.Code, r.Body)
		}
	}
	if _, err := h.db.Exec("INSERT INTO host_access(user_id,host_id,action) VALUES ('reader','private','host:read')"); err != nil {
		t.Fatal(err)
	}
	if r := h.request("GET", "/api/v1/hosts", "", true, false, ""); r.Code != 200 || !strings.Contains(r.Body.String(), "private") {
		t.Fatal("allowed host not listed")
	}
	if r := h.request("GET", "/api/v1/hosts/private/inventory", "", true, false, ""); r.Code != 404 {
		t.Fatal("host:read implicitly granted tmux:read")
	}
}

func TestHostOptionsAreAdminOnlyAndDoNotBypassAllowlist(t *testing.T) {
	h := setup(t)
	if r := h.request("GET", "/api/v1/hosts/options", "", false, false, ""); r.Code != 401 {
		t.Fatalf("unauthenticated options: %d", r.Code)
	}
	h.server.cfg.AllowedSSHHosts = map[string]bool{"z.example.ts.net": true, "100.100.100.10": true, "disabled.example.ts.net": false}
	r := h.request("GET", "/api/v1/hosts/options", "", true, false, "")
	if r.Code != 200 || strings.TrimSpace(r.Body.String()) != `{"allowedSSHHosts":["100.100.100.10","z.example.ts.net"]}` || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("options: %d %s", r.Code, r.Body)
	}
	h.server.cfg.AllowedSSHHosts = nil
	r = h.request("GET", "/api/v1/hosts/options", "", true, false, "")
	if r.Code != 200 || strings.TrimSpace(r.Body.String()) != `{"allowedSSHHosts":[]}` {
		t.Fatalf("empty options: %d %s", r.Code, r.Body)
	}
	r = h.request("POST", "/api/v1/hosts", `{"label":"Unapproved","transport":"ssh","tailnetAddress":"100.100.100.10","osUser":"dev"}`, true, true, h.csrf)
	if r.Code != 422 || !strings.Contains(r.Body.String(), "TMUX_MONITOR_ALLOWED_SSH_HOSTS") {
		t.Fatalf("missing actionable allowlist rejection: %d %s", r.Code, r.Body)
	}
	if _, err := h.db.Exec("UPDATE users SET is_admin=0"); err != nil {
		t.Fatal(err)
	}
	h.server.cfg.AllowedSSHHosts = map[string]bool{"private.example.ts.net": true}
	r = h.request("GET", "/api/v1/hosts/options", "", true, false, "")
	if r.Code != 403 || strings.Contains(r.Body.String(), "private.example.ts.net") {
		t.Fatalf("non-admin options leaked: %d %s", r.Code, r.Body)
	}
}

func TestAuditFailureRollsBackHostMutation(t *testing.T) {
	h := setup(t)
	if _, err := h.db.Exec("CREATE TRIGGER fail_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(FAIL,'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	r := h.request("POST", "/api/v1/hosts", `{"label":"No commit","transport":"agent","osUser":"dev"}`, true, true, h.csrf)
	if r.Code != 500 {
		t.Fatalf("audit failure ignored: %d", r.Code)
	}
	var count int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM hosts").Scan(&count); err != nil || count != 0 {
		t.Fatal("host committed without audit")
	}
}

func TestLoginRateLimitAndGenericFailures(t *testing.T) {
	h := setup(t)
	var first string
	for i := 0; i < 5; i++ {
		name := "missing"
		if i == 1 {
			name = "admin"
		}
		r := h.request("POST", "/api/v1/auth/login", `{"username":"`+name+`","password":"wrong"}`, false, true, "")
		if i < 4 {
			if r.Code != 401 {
				t.Fatalf("invalid login: %d", r.Code)
			}
			var v struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(r.Body.Bytes(), &v)
			if i == 0 {
				first = v.Error.Message
			}
			if v.Error.Message != first {
				t.Fatal("login reveals account existence")
			}
		} else if r.Code != 429 || r.Header().Get("Retry-After") == "" {
			t.Fatalf("rate limit missing %d", r.Code)
		}
	}
}

func TestStaticAndHealthDoNotRedirectOrCacheAPI(t *testing.T) {
	h := setup(t)
	for _, path := range []string{"/", "/hosts/anything", "/index.html"} {
		r := h.request("GET", path, "", false, false, "")
		if r.Code != 200 || !strings.Contains(r.Body.String(), "workspace") {
			t.Fatalf("SPA %s: %d %s", path, r.Code, r.Body)
		}
		if r.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
	if r := h.request("GET", "/assets/missing.js", "", false, false, ""); r.Code != 404 {
		t.Fatal("missing script returned HTML")
	}
	if r := h.request("GET", "/healthz", "", false, false, ""); r.Code != 200 {
		t.Fatalf("health %d", r.Code)
	}
	if r := h.request("GET", "/api/v1/me", "", false, false, ""); r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable unauthenticated API")
	}
	// Request bodies and storage errors must never leak into structured errors.
	if r := h.request("POST", "/api/v1/hosts", strings.Repeat("x", 9*1024), true, true, h.csrf); r.Code != 400 {
		t.Fatal("oversized request accepted")
	}
	if r := h.request("POST", "/api/v1/hosts", `{} {}`, true, true, h.csrf); r.Code != 400 {
		t.Fatal("multiple JSON bodies accepted")
	}
}
