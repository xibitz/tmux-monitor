package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type lifecycleFixture struct{ created, killed int }

func (f *lifecycleFixture) CreateSession(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, _ tmux.CreateSessionRequest, authorize func(context.Context) error) (string, error) {
	f.created++
	return "$12", authorize(ctx)
}

func (f *lifecycleFixture) KillSession(ctx context.Context, _ sshconnector.Host, _ sshconnector.Record, target string, _ tmux.KillSessionRequest, authorize func(context.Context) error) (string, error) {
	f.killed++
	return target, authorize(ctx)
}

func TestSessionLifecycleFlagsPermissionsAndDurableReplay(t *testing.T) {
	h, _ := renameHarness(t)
	fixture := new(lifecycleFixture)
	h.server.lifecycle = fixture
	const path = "/api/v1/hosts/ssh-test/sessions"
	call := func(method, path, key string, body any, want int, code string) {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, testOrigin+path, strings.NewReader(string(payload)))
		r.AddCookie(h.cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("X-CSRF-Token", h.csrf)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.server.ServeHTTP(w, r)
		if w.Code != want || (code != "" && !strings.Contains(w.Body.String(), code)) {
			t.Fatalf("HTTP %d, want %d %s: %s", w.Code, want, code, w.Body)
		}
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := h.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	create := tmux.CreateSessionRequest{Name: "fixture", Epoch: testEpoch, Revision: "cr_" + strings.Repeat("a", 64), Confirmed: true}
	createKey := strings.Repeat("a", 32)
	call(http.MethodPost, path, createKey, create, 503, "session_lifecycle_disabled")
	h.server.cfg.SessionLifecycleEnabled = true
	exec("INSERT INTO users(id,username,password_hash,created_at) VALUES ('member','member',?,?)", identity.DummyHash, storage.Now())
	exec("UPDATE app_sessions SET user_id='member'")
	exec("INSERT INTO host_access VALUES ('member','ssh-test','tmux:read')")
	call(http.MethodPost, path, createKey, create, 404, "")
	exec("INSERT INTO host_access VALUES ('member','ssh-test','tmux:write')")
	call(http.MethodPost, path, createKey, create, 201, `"sessionId":"$12"`)
	old := h.server
	h.server = New(h.db, old.cfg, old.assets, old.log)
	h.server.lifecycle = fixture
	call(http.MethodPost, path, createKey, create, 201, `"sessionId":"$12"`)
	create.Name = "changed"
	call(http.MethodPost, path, createKey, create, 409, "idempotency_conflict")
	kill := tmux.KillSessionRequest{Epoch: testEpoch, Revision: "r_" + strings.Repeat("b", 64), Confirmation: "$12", Confirmed: true}
	killKey := strings.Repeat("b", 32)
	call(http.MethodDelete, path+"/$12", killKey, kill, 404, "")
	exec("INSERT INTO host_access VALUES ('member','ssh-test','tmux:kill')")
	kill.Confirmation = "$13"
	call(http.MethodDelete, path+"/$12", killKey, kill, 422, "invalid_request")
	kill.Confirmation = "$12"
	call(http.MethodDelete, path+"/$12", killKey, kill, 200, `"sessionId":"$12"`)
	call(http.MethodDelete, path+"/$12", killKey, kill, 200, `"sessionId":"$12"`)
	if fixture.created != 1 || fixture.killed != 1 {
		t.Fatalf("unexpected repeated dispatch: %+v", fixture)
	}
}
