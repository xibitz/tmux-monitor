package httpapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

type pushTestTransport func(*http.Request) (*http.Response, error)

func (f pushTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNotificationsACLAndGenericPush(t *testing.T) {
	h := setup(t)
	h.server.cfg.CodingHooks, h.server.cfg.WebPush = true, true
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := h.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES ('member','member',?,?)`, identity.DummyHash, storage.Now())
	exec(`INSERT INTO hosts(id,label,transport,os_user,socket_name,created_at,updated_at)
		VALUES ('notice-host','Sensitive fixture host','agent','tester','default',?,?)`, storage.Now(), storage.Now())
	exec(`INSERT INTO coding_notices(host_id,source,kind,confidence,epoch,session_id,pane_id,dedupe,occurred_at)
		VALUES ('notice-host','claude','attention_required','unknown','','','','fixture',?)`, storage.Now())
	notice := func(allowed bool) {
		t.Helper()
		r := h.request("GET", "/api/v1/notifications", "", true, false, "")
		if r.Code != 200 || strings.Contains(r.Body.String(), "Sensitive fixture host") != allowed {
			t.Fatalf("inbox permission: %d %s", r.Code, r.Body)
		}
	}
	notice(true)
	exec("UPDATE app_sessions SET user_id='member'")
	notice(false)
	exec("INSERT INTO host_access VALUES ('member','notice-host','host:read')")
	notice(false)
	exec("INSERT INTO host_access VALUES ('member','notice-host','tmux:read')")
	notice(true)
	const endpoint = "https://fcm.googleapis.com/fcm/send/fixture-token"
	path := "/api/v1/notifications/push"
	body := `{"endpoint":"` + endpoint + `","enabled":true}`
	if r := h.request("POST", path, body, true, true, ""); r.Code != 403 {
		t.Fatalf("subscription without CSRF: %d", r.Code)
	}
	if r := h.request("POST", path, `{"endpoint":"https://127.0.0.1/send/fixture","enabled":true}`, true, true, h.csrf); r.Code != 422 {
		t.Fatalf("unknown provider accepted: %d", r.Code)
	}
	if r := h.request("POST", path, body, true, true, h.csrf); r.Code != 204 {
		t.Fatalf("subscribe: %d %s", r.Code, r.Body)
	}
	var sealed []byte
	if err := h.db.QueryRow("SELECT encrypted_endpoint FROM push_subscriptions").Scan(&sealed); err != nil || bytes.Contains(sealed, []byte(endpoint)) {
		t.Fatal("endpoint was not encrypted")
	}
	key, err := h.server.webPushKey()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(h.server.cfg.DataDir, "web-push.key"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("VAPID key must be owner-only")
	}
	calls := 0
	response := 201
	client := &http.Client{Transport: pushTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != endpoint || r.ContentLength != 0 || r.Body != nil || r.Header.Get("TTL") != "60" {
			t.Fatal("unexpected push request")
		}
		jwt, public, ok := strings.Cut(strings.TrimPrefix(r.Header.Get("Authorization"), "vapid t="), ", k=")
		parts := strings.Split(jwt, ".")
		if !ok || public != pushPublicKey(key) || len(parts) != 3 {
			t.Fatal("invalid VAPID header")
		}
		payload, err := pushEncoding.DecodeString(parts[1])
		var claims struct {
			Audience string `json:"aud"`
			Expires  int64  `json:"exp"`
		}
		if err != nil || json.Unmarshal(payload, &claims) != nil || claims.Audience != "https://fcm.googleapis.com" || claims.Expires <= time.Now().Unix() {
			t.Fatal("invalid VAPID claims")
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		sig, err := pushEncoding.DecodeString(parts[2])
		if err != nil || len(sig) != 64 || !ecdsa.Verify(&key.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			t.Fatal("invalid VAPID signature")
		}
		return &http.Response{StatusCode: response, Body: http.NoBody}, nil
	})}
	ctx := context.Background()
	exec("DELETE FROM host_access WHERE action='tmux:read'")
	h.server.deliverWebPush(ctx, "notice-host", client)
	if calls != 0 {
		t.Fatal("push delivered after access revoked")
	}
	exec("INSERT INTO host_access VALUES ('member','notice-host','tmux:read')")
	h.server.deliverWebPush(ctx, "notice-host", client)
	h.server.deliverWebPush(ctx, "notice-host", client)
	if calls != 1 {
		t.Fatalf("expected one push with cooldown, got %d", calls)
	}
	exec("UPDATE push_subscriptions SET last_sent_at=''")
	response = 410
	h.server.deliverWebPush(ctx, "notice-host", client)
	var remaining int
	if err := h.db.QueryRow("SELECT count(*) FROM push_subscriptions").Scan(&remaining); err != nil || calls != 2 || remaining != 0 {
		t.Fatal("expired subscription was not removed")
	}
}

func TestCodingNoticeRetentionAndRevokedGeneration(t *testing.T) {
	h := setup(t)
	host, id := storage.ID("h_"), storage.ID("a_")
	if _, err := h.db.Exec(`INSERT INTO hosts(id,label,transport,os_user,socket_name,created_at,updated_at)
		VALUES (?, 'Fixture', 'agent', 'tester', 'default', ?, ?)`, host, storage.Now(), storage.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO agent_identities(id,host_id,credential_hash,tailnet_ip,generation,created_at)
		VALUES (?,?,?,'100.64.0.1',2,?)`, id, host, identity.TokenHash("fixture"), storage.Now()); err != nil {
		t.Fatal(err)
	}
	peer := &agentPeer{id: id, hostID: host, osUser: "tester", socket: "default", generation: 2, ctx: context.Background(), ready: make(chan struct{})}
	close(peer.ready)
	notice := agentproto.CodingMessage{CodingNotice: agentproto.CodingNotice{Schema: 1, Source: "codex", Kind: "turn_completed"}}
	for i := 0; i < 202; i++ {
		notice.Dedupe = fmt.Sprintf("%064x", i)
		h.server.recordCodingNotice(peer, notice)
	}
	h.server.recordCodingNotice(peer, notice) // The latest event is still deduplicated.
	if _, err := h.db.Exec("UPDATE agent_identities SET generation=3 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	notice.Dedupe = strings.Repeat("b", 64)
	h.server.recordCodingNotice(peer, notice)
	var total, first, last int
	err := h.db.QueryRow("SELECT count(*), min(id), max(id) FROM coding_notices WHERE host_id=?", host).Scan(&total, &first, &last)
	if err != nil || total != 200 || first != 3 || last != 202 {
		t.Fatalf("notice retention/deduplication/fence: %d (%d..%d), %v", total, first, last, err)
	}
}
