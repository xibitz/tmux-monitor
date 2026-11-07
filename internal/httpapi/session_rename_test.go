package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type renameFunc func(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.RenameRequest, func(context.Context) error) error

func (f renameFunc) RenameSession(c context.Context, h sshconnector.Host, r sshconnector.Record, s string, q tmux.RenameRequest, a func(context.Context) error) error {
	return f(c, h, r, s, q, a)
}
func renameHarness(t *testing.T) (*harness, string) {
	h := setup(t)
	seedSSH(t, h)
	h.server.cfg.MutationsEnabled = true
	h.server.cfg.AllowedSSHHosts["test.example.ts.net"] = true
	b, _ := json.Marshal(tmux.RenameRequest{Name: "renamed-private-name", Epoch: testEpoch, Revision: "rn_" + strings.Repeat("b", 64), Confirmed: true})
	return h, string(b)
}
func renameHTTP(h *harness, body, key string, auth, origin, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PATCH", testOrigin+"/api/v1/hosts/ssh-test/sessions/$0", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	if auth {
		r.AddCookie(h.cookie)
	}
	if origin {
		r.Header.Set("Origin", testOrigin)
	}
	if csrf {
		r.Header.Set("X-CSRF-Token", h.csrf)
	}
	w := httptest.NewRecorder()
	h.server.ServeHTTP(w, r)
	return w
}

func TestSessionRenameSecurityIdempotencyAndRestart(t *testing.T) {
	h, body := renameHarness(t)
	key := strings.Repeat("c", 32)
	var calls atomic.Int32
	h.server.mutations = renameFunc(func(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _ string, _ tmux.RenameRequest, check func(context.Context) error) error {
		calls.Add(1)
		return check(ctx)
	})
	h.server.cfg.MutationsEnabled = false
	if r := renameHTTP(h, body, key, true, true, true); r.Code != 503 {
		t.Fatal("default gate bypass")
	}
	h.server.cfg.MutationsEnabled = true
	for _, c := range []struct {
		auth, origin, csrf bool
		want               int
	}{{false, true, true, 401}, {true, false, true, 403}, {true, true, false, 403}} {
		if r := renameHTTP(h, body, key, c.auth, c.origin, c.csrf); r.Code != c.want {
			t.Fatalf("boundary %d %s", r.Code, r.Body)
		}
	}
	for _, bad := range []string{strings.Replace(body, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(body, `"name":"renamed-private-name"`, `"name":"x;kill-server"`, 1), strings.TrimSuffix(body, "}") + `,"shell":"id"}`} {
		if r := renameHTTP(h, bad, key, true, true, true); r.Code != 422 && !(r.Code == 400 && strings.Contains(bad, `"shell"`)) {
			t.Fatalf("validation %d %s", r.Code, r.Body)
		}
	}
	first := renameHTTP(h, body, key, true, true, true)
	if first.Code != 200 || calls.Load() != 1 {
		t.Fatalf("first %d %s", first.Code, first.Body)
	}
	replay := renameHTTP(h, body, key, true, true, true)
	if replay.Code != 200 || first.Body.String() != replay.Body.String() || calls.Load() != 1 {
		t.Fatal("request executed again")
	}
	if r := renameHTTP(h, strings.Replace(body, "renamed-private-name", "other", 1), key, true, true, true); r.Code != 409 || !strings.Contains(r.Body.String(), "idempotency_conflict") {
		t.Fatal("key rebound")
	}
	previous := h.server
	h.server = New(h.db, previous.cfg, previous.assets, previous.log)
	h.server.mutations = renameFunc(func(context.Context, sshconnector.Host, sshconnector.Record, string, tmux.RenameRequest, func(context.Context) error) error {
		calls.Add(1)
		return nil
	})
	if r := renameHTTP(h, body, key, true, true, true); r.Code != 200 || calls.Load() != 1 {
		t.Fatal("restart lost completed idempotency")
	}
	if _, err := h.db.Exec(`UPDATE tmux_operations SET state='pending'`); err != nil {
		t.Fatal(err)
	}
	if r := renameHTTP(h, body, key, true, true, true); r.Code != 409 || !strings.Contains(r.Body.String(), "operation_uncertain") || calls.Load() != 1 {
		t.Fatal("restart executed pending request")
	}
	rows, err := h.db.Query(`SELECT key_hash,request_hash FROM tmux_operations`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a, b string
		if err = rows.Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		if a == key || strings.Contains(a+b, "renamed-private-name") {
			t.Fatal("raw request persisted")
		}
	}
	if strings.Contains(h.logs.String(), "renamed-private-name") {
		t.Fatal("name in logs")
	}
}

func TestSessionRenameConcurrentAndAuditFailure(t *testing.T) {
	h, body := renameHarness(t)
	key := strings.Repeat("d", 32)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	h.server.mutations = renameFunc(func(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _ string, _ tmux.RenameRequest, check func(context.Context) error) error {
		calls.Add(1)
		close(entered)
		<-release
		return check(ctx)
	})
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- renameHTTP(h, body, key, true, true, true) }()
	<-entered
	if r := renameHTTP(h, body, key, true, true, true); r.Code != 409 || calls.Load() != 1 {
		t.Fatal("concurrent duplicate dispatched")
	}
	close(release)
	if r := <-result; r.Code != 200 {
		t.Fatalf("first %d %s", r.Code, r.Body)
	}
	if _, err := h.db.Exec(`CREATE TRIGGER reject_intent BEFORE INSERT ON audit_events WHEN NEW.action='tmux.session.rename' BEGIN SELECT RAISE(FAIL,'test audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if r := renameHTTP(h, body, strings.Repeat("e", 32), true, true, true); r.Code != 500 || calls.Load() != 1 {
		t.Fatal("dispatch without durable audit")
	}
}

func TestSessionRenameFinalizationFailureAndPermissionRevoke(t *testing.T) {
	h, body := renameHarness(t)
	var calls atomic.Int32
	h.server.mutations = renameFunc(func(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _ string, _ tmux.RenameRequest, check func(context.Context) error) error {
		if err := check(ctx); err != nil {
			return err
		}
		calls.Add(1)
		return nil
	})
	if _, err := h.db.Exec(`CREATE TRIGGER reject_completion BEFORE UPDATE ON tmux_operations BEGIN SELECT RAISE(FAIL,'test completion unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 32)
	for i := 0; i < 2; i++ {
		r := renameHTTP(h, body, key, true, true, true)
		if r.Code != 409 || !strings.Contains(r.Body.String(), "operation_uncertain") {
			t.Fatalf("ambiguous result %d %s", r.Code, r.Body)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("ambiguous result retried")
	}
	if _, err := h.db.Exec(`DROP TRIGGER reject_completion`); err != nil {
		t.Fatal(err)
	}
	h.server.mutations = renameFunc(func(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _ string, _ tmux.RenameRequest, check func(context.Context) error) error {
		if _, err := h.db.Exec(`DELETE FROM ssh_credentials WHERE host_id='ssh-test'`); err != nil {
			t.Fatal(err)
		}
		if err := check(ctx); err != nil {
			return &sshconnector.Failure{Code: "access_revoked"}
		}
		calls.Add(1)
		return nil
	})
	if r := renameHTTP(h, body, strings.Repeat("b", 32), true, true, true); r.Code != 403 || calls.Load() != 1 {
		t.Fatal("revoked configuration dispatched")
	}
}

func TestSessionRenameNonAdminRequiresReadAndWrite(t *testing.T) {
	h, body := renameHarness(t)
	if _, err := h.db.Exec(`UPDATE users SET is_admin=0`); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := h.db.QueryRow(`SELECT user_id FROM app_sessions WHERE id_hash=?`, identity.TokenHash(h.cookie.Value)).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO host_access(user_id,host_id,action) VALUES (?,'ssh-test','tmux:read')`, actor); err != nil {
		t.Fatal(err)
	}
	if r := renameHTTP(h, body, strings.Repeat("a", 32), true, true, true); r.Code != 404 {
		t.Fatal("read-only ACL bypass")
	}
	if r := h.request("GET", "/api/v1/hosts/ssh-test/session-options", "", true, false, ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"canRename":false`) {
		t.Fatal("wrong options")
	}
	if _, err := h.db.Exec(`INSERT INTO host_access(user_id,host_id,action) VALUES (?,'ssh-test','tmux:write')`, actor); err != nil {
		t.Fatal(err)
	}
	h.server.mutations = renameFunc(func(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _ string, _ tmux.RenameRequest, check func(context.Context) error) error {
		return check(ctx)
	})
	if r := renameHTTP(h, body, strings.Repeat("a", 32), true, true, true); r.Code != 200 {
		t.Fatalf("write grant rejected %d %s", r.Code, r.Body)
	}
}

func TestSessionRenameLedgerBoundAndNoPruning(t *testing.T) {
	h, body := renameHarness(t)
	_, err := h.db.Exec(`WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<10000)
 INSERT INTO tmux_operations(actor_id,key_hash,request_hash,operation_id,host_id,state,created_at,updated_at)
 SELECT 'fixture',printf('%064x',n),'request','op_'||n,'ssh-test','pending','2020-01-01','2020-01-01' FROM seq`)
	if err != nil {
		t.Fatal(err)
	}
	if r := renameHTTP(h, body, strings.Repeat("f", 32), true, true, true); r.Code != 429 || !strings.Contains(r.Body.String(), "operation_limit") {
		t.Fatalf("quota %d %s", r.Code, r.Body)
	}
	var count int
	if err = h.db.QueryRow(`SELECT count(*) FROM tmux_operations`).Scan(&count); err != nil || count != 10000 {
		t.Fatal("old reservations pruned automatically")
	}
}
