package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type observerFunc func(context.Context, sshconnector.Host, sshconnector.Record) (tmux.Inventory, error)

func (f observerFunc) Snapshot(ctx context.Context, h sshconnector.Host, r sshconnector.Record) (tmux.Inventory, error) {
	return f(ctx, h, r)
}

func seedSSH(t *testing.T, h *harness) {
	t.Helper()
	if _, err := h.db.Exec("INSERT INTO hosts(id,label,transport,tailnet_address,os_user,socket_name,created_at,updated_at) VALUES ('ssh-test','Test','ssh','test.example.ts.net','tester','default',?,?)", storage.Now(), storage.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec("INSERT INTO ssh_credentials(host_id,credential_id,port,key_ref,host_public_key,host_key_fingerprint,configured_at) VALUES ('ssh-test','cred_test',22,'k_00000000000000000000000000000000','PUBLIC-ONLY','SHA256:public',?)", storage.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestSSHProbeCacheStalenessAndMetadataPrivacy(t *testing.T) {
	h := setup(t)
	seedSSH(t, h)
	h.server.ssh = observerFunc(func(_ context.Context, host sshconnector.Host, _ sshconnector.Record) (tmux.Inventory, error) {
		return tmux.Empty(host.ID, host.Socket, "3.8"), nil
	})
	r := h.request("GET", "/api/v1/hosts/ssh-test/ssh", "", true, false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"configured":true`) {
		t.Fatalf("metadata %d %s", r.Code, r.Body)
	}
	for _, secret := range []string{"PUBLIC-ONLY", "k_0000", "key_ref", "keyPath"} {
		if strings.Contains(r.Body.String(), secret) {
			t.Fatal("secret reference leaked")
		}
	}
	if r := h.request("POST", "/api/v1/hosts/ssh-test/probe", "", true, true, ""); r.Code != 403 {
		t.Fatal("probe accepted without CSRF")
	}
	r = h.request("POST", "/api/v1/hosts/ssh-test/probe", "", true, true, h.csrf)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"state":"ONLINE"`) {
		t.Fatalf("probe: %d %s", r.Code, r.Body)
	}
	var body struct {
		Inventory tmux.Inventory `json:"inventory"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	observedAt := body.Inventory.ObservedAt
	if body.Inventory.Stale || observedAt == "" {
		t.Fatal("fresh observation missing timestamp")
	}
	h.server.ssh = observerFunc(func(context.Context, sshconnector.Host, sshconnector.Record) (tmux.Inventory, error) {
		return tmux.Inventory{}, &sshconnector.Failure{Code: "timeout"}
	})
	r = h.request("GET", "/api/v1/hosts/ssh-test/inventory", "", true, false, "")
	var cached tmux.Inventory
	if err := json.Unmarshal(r.Body.Bytes(), &cached); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || !cached.Stale || cached.ObservedAt != observedAt || cached.State != "UNREACHABLE" || cached.StatusReason != "timeout" {
		t.Fatalf("stale result: %d %+v", r.Code, cached)
	}
	r = h.request("GET", "/api/v1/hosts", "", true, false, "")
	if !strings.Contains(r.Body.String(), `"statusReason":"timeout"`) || !strings.Contains(r.Body.String(), `"stale":true`) {
		t.Fatalf("host state: %s", r.Body)
	}
	r = h.request("GET", "/api/v1/audit", "", true, false, "")
	if !strings.Contains(r.Body.String(), "host.probe") || strings.Contains(r.Body.String(), "PUBLIC-ONLY") {
		t.Fatal("invalid probe audit")
	}
}

func TestSSHResultsDiscardedAfterRevokeAndHostACL(t *testing.T) {
	h := setup(t)
	seedSSH(t, h)
	h.server.ssh = observerFunc(func(ctx context.Context, host sshconnector.Host, _ sshconnector.Record) (tmux.Inventory, error) {
		_, err := h.db.ExecContext(ctx, "DELETE FROM ssh_credentials WHERE host_id=?", host.ID)
		if err != nil {
			return tmux.Inventory{}, err
		}
		return tmux.Empty(host.ID, host.Socket, "3.8"), nil
	})
	r := h.request("POST", "/api/v1/hosts/ssh-test/probe", "", true, true, h.csrf)
	if r.Code != 422 || !strings.Contains(r.Body.String(), "credentials_required") {
		t.Fatalf("revoked result accepted: %d %s", r.Code, r.Body)
	}
	var count int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM inventory_cache").Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked result populated cache")
	}
	if err := h.db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action='host.probe' AND result='credentials_required'").Scan(&count); err != nil || count != 1 {
		t.Fatal("revoked in-flight probe was not audited")
	}
	if _, err := h.db.Exec("UPDATE users SET is_admin=0"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/hosts/ssh-test/ssh", "/api/v1/hosts/ssh-test/probe"} {
		method := "GET"
		if strings.HasSuffix(path, "probe") {
			method = "POST"
		}
		if r := h.request(method, path, "", true, true, h.csrf); r.Code != 403 {
			t.Fatalf("non-admin SSH operation accepted: %s %d", path, r.Code)
		}
	}
	if r := h.request("GET", "/api/v1/hosts/ssh-test/inventory", "", true, false, ""); r.Code != 404 {
		t.Fatal("unauthorized SSH inventory leaked")
	}
}

func TestProbeAuditFailureRollsBackObservation(t *testing.T) {
	h := setup(t)
	seedSSH(t, h)
	h.server.ssh = observerFunc(func(_ context.Context, host sshconnector.Host, _ sshconnector.Record) (tmux.Inventory, error) {
		return tmux.Empty(host.ID, host.Socket, "3.8"), nil
	})
	if _, err := h.db.Exec("CREATE TRIGGER reject_probe BEFORE INSERT ON audit_events WHEN NEW.action='host.probe' BEGIN SELECT RAISE(FAIL,'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	if r := h.request("POST", "/api/v1/hosts/ssh-test/probe", "", true, true, h.csrf); r.Code != 500 {
		t.Fatal("audit failure ignored")
	}
	var count int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM inventory_cache").Scan(&count); err != nil || count != 0 {
		t.Fatal("inventory committed without audit")
	}
	if err := h.db.QueryRow("SELECT COUNT(*) FROM host_observations").Scan(&count); err != nil || count != 0 {
		t.Fatal("observation committed without audit")
	}
}

func TestCorruptOrCrossHostCacheCannotBeReturned(t *testing.T) {
	h := setup(t)
	seedSSH(t, h)
	h.server.ssh = observerFunc(func(context.Context, sshconnector.Host, sshconnector.Record) (tmux.Inventory, error) {
		return tmux.Inventory{}, &sshconnector.Failure{Code: "timeout"}
	})
	for _, body := range []string{`{}`, `not-json`, `{"hostId":"another-host","socketId":"default","transport":"ssh","observedAt":"2026-11-06T01:00:00Z","sessions":[],"statusReason":"PRIVATE-WRONG-HOST"}`} {
		if _, err := h.db.Exec("INSERT INTO inventory_cache(host_id,snapshot_json,observed_at) VALUES ('ssh-test',?,?) ON CONFLICT(host_id) DO UPDATE SET snapshot_json=excluded.snapshot_json", body, storage.Now()); err != nil {
			t.Fatal(err)
		}
		r := h.request("GET", "/api/v1/hosts/ssh-test/inventory", "", true, false, "")
		if r.Code != 423 || strings.Contains(r.Body.String(), "PRIVATE-WRONG-HOST") || !strings.Contains(r.Body.String(), "timeout") {
			t.Fatalf("invalid cached snapshot returned: %d %s", r.Code, r.Body)
		}
	}
}
