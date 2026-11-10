package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func (s *Server) webPushKey() (*ecdsa.PrivateKey, error) {
	s.pushKeyMu.Lock()
	defer s.pushKeyMu.Unlock()
	path := filepath.Join(s.cfg.DataDir, "web-push.key")
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		temp, err := os.CreateTemp(s.cfg.DataDir, ".web-push-")
		if err != nil {
			return nil, err
		}
		defer os.Remove(temp.Name())
		defer temp.Close()
		var raw [32]byte
		defer clear(raw[:])
		if _, err := temp.Write(key.D.FillBytes(raw[:])); err != nil {
			return nil, err
		}
		if err := temp.Sync(); err != nil {
			return nil, err
		}
		if err := temp.Close(); err != nil {
			return nil, err
		}
		if err := os.Link(temp.Name(), path); err != nil {
			return nil, err
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != 32 {
		return nil, errors.New("invalid Web Push key file")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("invalid Web Push key owner")
	}
	var raw [32]byte
	defer clear(raw[:])
	if _, err := io.ReadFull(file, raw[:]); err != nil {
		return nil, err
	}
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), raw[:])
}

var pushEncoding = base64.RawURLEncoding

func pushPublicKey(key *ecdsa.PrivateKey) string {
	return pushEncoding.EncodeToString(elliptic.Marshal(key.Curve, key.X, key.Y))
}

func pushCipher(key *ecdsa.PrivateKey) (cipher.AEAD, error) {
	var raw [32]byte
	defer clear(raw[:])
	key.D.FillBytes(raw[:])
	digest := sha256.Sum256(append([]byte("tmux-monitor:push-endpoint:v1:"), raw[:]...))
	defer clear(digest[:])
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

// Only the HTTPS push services explicitly supported by this integration are
// dialed. Endpoints supplied by browsers are capabilities, never logged.
var pushToken = regexp.MustCompile(`^[A-Za-z0-9._~:-]+$`)

func pushEndpoint(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.User != nil ||
		u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(u.Path) < 2 {
		return nil
	}
	prefix := ""
	switch u.Host {
	case "fcm.googleapis.com":
		prefix = "/fcm/send/"
	case "updates.push.services.mozilla.com":
		prefix = "/wpush/v2/"
	case "web.push.apple.com":
		prefix = "/"
	}
	if prefix == "" || u.RawPath != "" || !strings.HasPrefix(u.Path, prefix) || !pushToken.MatchString(strings.TrimPrefix(u.Path, prefix)) {
		return nil
	}
	return u
}

func (s *Server) pushOptions(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.WebPush {
		s.json(w, http.StatusOK, map[string]bool{"enabled": false})
		return
	}
	key, err := s.webPushKey()
	if err != nil {
		s.internal(w, r)
		return
	}
	s.json(w, http.StatusOK, map[string]any{"enabled": true, "publicKey": pushPublicKey(key)})
}

func (s *Server) updatePushSubscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
		Enabled  bool   `json:"enabled"`
	}
	if !s.decode(w, r, &body) {
		return
	}
	endpoint := pushEndpoint(body.Endpoint)
	if endpoint == nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "invalid_request", "Layanan push browser belum didukung.")
		return
	}
	hash := sha256.Sum256([]byte(endpoint.String()))
	id, actor := hex.EncodeToString(hash[:]), user(r)
	ctx := r.Context()
	if !body.Enabled {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM push_subscriptions WHERE id=? AND user_id=?", id, actor.ID); err != nil {
			s.internal(w, r)
			return
		}
		s.json(w, http.StatusNoContent, nil)
		return
	}
	if !s.cfg.WebPush {
		s.fail(w, r, http.StatusForbidden, "forbidden", "Web Push belum diaktifkan.")
		return
	}
	key, err := s.webPushKey()
	if err != nil {
		s.internal(w, r)
		return
	}
	seal, err := pushCipher(key)
	if err != nil {
		s.internal(w, r)
		return
	}
	secret := seal.Seal(nil, nil, []byte(endpoint.String()), []byte(id))
	result, err := s.db.ExecContext(ctx, `INSERT INTO push_subscriptions(id,user_id,encrypted_endpoint)
		SELECT ?,?,? WHERE
		EXISTS(SELECT 1 FROM push_subscriptions WHERE id=? AND user_id=?)
		OR ((SELECT count(*) FROM push_subscriptions WHERE user_id=?)<4 AND
			(SELECT count(*) FROM push_subscriptions)<256)
		ON CONFLICT(id) DO UPDATE SET encrypted_endpoint=excluded.encrypted_endpoint
		WHERE push_subscriptions.user_id=excluded.user_id`, id, actor.ID, secret, id, actor.ID, actor.ID)
	if err != nil {
		s.internal(w, r)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		s.internal(w, r)
		return
	}
	if affected == 0 {
		s.fail(w, r, http.StatusConflict, "conflict", "Endpoint sudah terdaftar pada akun lain atau batas perangkat tercapai.")
		return
	}
	s.json(w, http.StatusNoContent, nil)
}

func (s *Server) queueWebPush(hostID string) {
	select {
	case s.pushSlots <- struct{}{}:
	default:
		return // Inbox remains the source of truth when push is saturated.
	}
	go func() {
		defer func() { <-s.pushSlots }()
		ctx, cancel := context.WithTimeout(s.terminalRoot, 25*time.Second)
		defer cancel()
		client := &http.Client{
			Transport: &http.Transport{DialContext: dialPush},
			Timeout:   4 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		defer client.CloseIdleConnections()
		s.deliverWebPush(ctx, hostID, client)
	}()
}

func (s *Server) deliverWebPush(ctx context.Context, hostID string, client *http.Client) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id FROM push_subscriptions p
		JOIN users u ON u.id=p.user_id JOIN hosts h ON h.id=? WHERE u.disabled_at IS NULL
		AND (u.is_admin=1 OR (SELECT count(*) FROM host_access
		WHERE host_id=h.id AND user_id=u.id AND action IN ('host:read','tmux:read'))=2)
		ORDER BY p.last_sent_at,p.id LIMIT 32`, hostID)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 || rows.Err() != nil {
		return
	}
	key, err := s.webPushKey()
	if err != nil {
		return
	}
	seal, err := pushCipher(key)
	if err != nil {
		return
	}

	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		sealed, err := s.readPushEndpoint(ctx, hostID, id)
		if err != nil || len(sealed) == 0 {
			continue
		}
		plain, err := seal.Open(nil, nil, sealed, []byte(id))
		if err != nil {
			continue
		}
		endpoint := pushEndpoint(string(plain))
		clear(plain)
		if endpoint == nil {
			continue
		}
		auth, err := pushAuthorization(key, endpoint)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", auth)
		req.Header.Set("TTL", "60")
		req.Header.Set("Topic", "tmux-monitor-inbox")
		response, err := client.Do(req)
		if err != nil {
			continue
		}
		response.Body.Close()
		if response.StatusCode == http.StatusGone || response.StatusCode == http.StatusNotFound {
			_, _ = s.db.ExecContext(ctx, "DELETE FROM push_subscriptions WHERE id=? AND encrypted_endpoint=?", id, sealed)
		}
	}
}

var sharedPushNetwork = netip.MustParsePrefix("100.64.0.0/10")

func dialPush(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	for _, ip := range ips {
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || sharedPushNetwork.Contains(ip) {
			continue
		}
		if conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port)); err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("public push service unavailable")
}

func pushAuthorization(key *ecdsa.PrivateKey, endpoint *url.URL) (string, error) {
	const header = `{"typ":"JWT","alg":"ES256"}`
	claims, err := json.Marshal(map[string]any{
		"aud": "https://" + endpoint.Host,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}
	signed := pushEncoding.EncodeToString([]byte(header)) + "." + pushEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	var signature [64]byte
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return fmt.Sprintf("vapid t=%s.%s, k=%s", signed, pushEncoding.EncodeToString(signature[:]), pushPublicKey(key)), nil
}
