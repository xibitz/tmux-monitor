package sshconnector

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/tmux"
	"golang.org/x/crypto/ssh"
)

const sample = "F|123|1700000000|/tmp/tmux-1000/default|3.8\nS\t$0\twork\t1700000001\nW\t$0\t@0\teditor\t0\t1\nP\t@0\t%0\t1\t120\t39\tzsh\nF|123|1700000000|/tmp/tmux-1000/default|3.8\n"

type fixture struct {
	store                    Store
	record                   Record
	hostSigner, clientSigner ssh.Signer
	host                     Host
	keyPath, publicPath      string
}

func signer(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, pem.EncodeToMemory(block)
}
func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db, storage.LatestVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO hosts(id,label,transport,tailnet_address,os_user,socket_name,created_at,updated_at) VALUES ('h_test','Test','ssh','test.example.ts.net','tester','default',?,?)", storage.Now(), storage.Now()); err != nil {
		t.Fatal(err)
	}
	hostSigner, _ := signer(t)
	clientSigner, private := signer(t)
	keyPath, publicPath := filepath.Join(dir, "private"), filepath.Join(dir, "host.pub")
	if err := os.WriteFile(keyPath, private, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, ssh.MarshalAuthorizedKey(hostSigner.PublicKey()), 0600); err != nil {
		t.Fatal(err)
	}
	store := Store{DB: db, DataDir: filepath.Join(dir, "data")}
	if _, err := store.Configure(context.Background(), "h_test", keyPath, publicPath, ssh.FingerprintSHA256(hostSigner.PublicKey()), 22); err != nil {
		t.Fatal(err)
	}
	record, err := store.Load(context.Background(), "h_test")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store, record, hostSigner, clientSigner, Host{ID: "h_test", Address: "test.example.ts.net", User: "tester", Socket: "default"}, keyPath, publicPath}
}

type reply struct {
	out, stderr string
	status      uint32
}

func normal(command string) reply {
	snapshot, _ := tmux.SnapshotCommand("default")
	switch command {
	case "LC_ALL=C id -un":
		return reply{out: "tester\n"}
	case "LC_ALL=C tmux -V":
		return reply{out: "tmux 3.8\n"}
	case snapshot:
		return reply{out: sample}
	}
	return reply{stderr: "unknown command", status: 127}
}

// A real SSH protocol server with generated keys; only test fixtures execute.
// It never connects to configured user hosts or reads a user's SSH directory.
func server(t *testing.T, key ssh.Signer, authorized ssh.PublicKey, handler func(string) reply, extraKeys ...ssh.Signer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(key.Marshal(), authorized.Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("not authorized")
	}}
	config.AddHostKey(key)
	for _, extra := range extraKeys {
		config.AddHostKey(extra)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections[conn] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				defer func() { mu.Lock(); delete(connections, conn); mu.Unlock() }()
				_, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					if ch.ChannelType() != "session" {
						_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					channel, reqs, err := ch.Accept()
					if err != nil {
						return
					}
					for req := range reqs {
						if req.Type != "exec" {
							_ = req.Reply(false, nil)
							continue
						}
						var exec struct{ Command string }
						if err := ssh.Unmarshal(req.Payload, &exec); err != nil {
							_ = req.Reply(false, nil)
							break
						}
						_ = req.Reply(true, nil)
						r := handler(exec.Command)
						_, _ = channel.Write([]byte(r.out))
						_, _ = channel.Stderr().Write([]byte(r.stderr))
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{r.status}))
						_ = channel.Close()
						break
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return listener.Addr().String()
}

func TestPinnedKeySelectedWhenServerOffersMultipleAlgorithms(t *testing.T) {
	f := setup(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), normal, other)
	if _, err := manager(f, address).Snapshot(context.Background(), f.host, f.record); err != nil {
		t.Fatalf("pinned Ed25519 key not selected: %v", err)
	}
}

func TestRSAHostAndClientKeysUseSHA2(t *testing.T) {
	f := setup(t)
	hostPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(clientPrivate, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.publicPath, ssh.MarshalAuthorizedKey(hostSigner.PublicKey()), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Configure(context.Background(), f.host.ID, f.keyPath, f.publicPath, ssh.FingerprintSHA256(hostSigner.PublicKey()), 22); err != nil {
		t.Fatal(err)
	}
	f.record, err = f.store.Load(context.Background(), f.host.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := f.store.Key(f.record)
	if err != nil {
		t.Fatal(err)
	}
	algorithms := key.(ssh.MultiAlgorithmSigner).Algorithms()
	for _, algorithm := range algorithms {
		if algorithm == ssh.KeyAlgoRSA {
			t.Fatal("legacy SHA1 signature enabled")
		}
	}
	secureHost, err := ssh.NewSignerWithAlgorithms(hostSigner.(ssh.AlgorithmSigner), []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512})
	if err != nil {
		t.Fatal(err)
	}
	address := server(t, secureHost, clientSigner.PublicKey(), normal)
	if _, err := manager(f, address).Snapshot(context.Background(), f.host, f.record); err != nil {
		t.Fatalf("RSA SHA2 handshake: %v", err)
	}
}
func manager(f *fixture, target string) *Manager {
	m := New(f.store, map[string]bool{f.host.Address: true})
	m.resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("100.64.1.2")}, nil
	}
	m.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "100.64.1.2:22" {
			return nil, fmt.Errorf("unvalidated dial address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}
	return m
}

func TestSSHObservationAndNoServer(t *testing.T) {
	f := setup(t)
	var mu sync.Mutex
	commands := []string{}
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), func(command string) reply {
		mu.Lock()
		commands = append(commands, command)
		mu.Unlock()
		return normal(command)
	})
	m := manager(f, address)
	inv, err := m.Snapshot(context.Background(), f.host, f.record)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Epoch == "" || len(inv.Sessions) != 1 || inv.Sessions[0].Windows[0].Panes[0].ID != "%0" {
		t.Fatalf("snapshot: %+v", inv)
	}
	mu.Lock()
	if len(commands) != 3 {
		t.Errorf("unexpected command count: %d", len(commands))
	}
	mu.Unlock()
	emptyAddress := server(t, f.hostSigner, f.clientSigner.PublicKey(), func(command string) reply {
		if strings.Contains(command, "list-sessions") {
			return reply{stderr: "no server running on /tmp/tmux-1000/default\n", status: 1}
		}
		return normal(command)
	})
	inv, err = manager(f, emptyAddress).Snapshot(context.Background(), f.host, f.record)
	if err != nil || inv.State != "ONLINE" || inv.Epoch != "" || len(inv.Sessions) != 0 || inv.Capabilities.ServerRunning {
		t.Fatalf("empty server: %+v %v", inv, err)
	}
}

func TestSSHRejectsHostKeyAuthAndNonTailnet(t *testing.T) {
	f := setup(t)
	other, _ := signer(t)
	for _, tc := range []struct {
		name       string
		key        ssh.Signer
		authorized ssh.PublicKey
		want       string
	}{{"host key", other, f.clientSigner.PublicKey(), "host_key_mismatch"}, {"auth", f.hostSigner, other.PublicKey(), "authentication_failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			address := server(t, tc.key, tc.authorized, normal)
			_, err := manager(f, address).Snapshot(context.Background(), f.host, f.record)
			if Code(err) != tc.want {
				t.Fatalf("want %s got %v", tc.want, err)
			}
		})
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "8.8.8.8", "::1", "2001:db8::1", "::ffff:127.0.0.1"} {
		m := manager(f, "unused")
		m.resolve = func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(ip)}, nil }
		_, err := m.Snapshot(context.Background(), f.host, f.record)
		if Code(err) != "address_not_tailnet" {
			t.Fatalf("non-tailnet %s accepted: %v", ip, err)
		}
	}
	m := manager(f, "unused")
	m.Allowed = map[string]bool{}
	if _, err := m.Snapshot(context.Background(), f.host, f.record); Code(err) != "address_not_allowed" {
		t.Fatal("allowlist bypassed")
	}
}

func TestSSHTimeoutLimitsAndSanitizedErrors(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name     string
		response reply
		want     string
	}{{"large", reply{out: strings.Repeat("x", tmux.MaxOutput+1)}, "output_limit"}, {"malformed", reply{out: "PRIVATE-MALFORMED-OUTPUT"}, "inventory_invalid"}, {"failure", reply{stderr: "PRIVATE-STDERR-MUST-NOT-LEAK", status: 1}, "command_failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			address := server(t, f.hostSigner, f.clientSigner.PublicKey(), func(command string) reply {
				if strings.Contains(command, "list-sessions") {
					return tc.response
				}
				return normal(command)
			})
			_, err := manager(f, address).Snapshot(context.Background(), f.host, f.record)
			if Code(err) != tc.want || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), normal)
	m := manager(f, address)
	m.Timeout = time.Nanosecond
	if _, err := m.Snapshot(context.Background(), f.host, f.record); Code(err) != "timeout" {
		t.Fatalf("timeout not honored: %v", err)
	}
}

func TestCredentialPermissionsRotationAndRevoke(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	path, err := f.store.path(f.record.KeyRef)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe stored key mode")
	}
	if _, err := f.store.path("../../private"); err == nil {
		t.Fatal("unsafe key reference accepted")
	}
	if _, err := f.store.Configure(ctx, f.host.ID, f.keyPath, f.publicPath, "SHA256:wrong", 22); err == nil {
		t.Fatal("unverified host key accepted")
	}
	if err := os.Chmod(f.keyPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Configure(ctx, f.host.ID, f.keyPath, f.publicPath, f.record.Fingerprint, 22); err == nil {
		t.Fatal("world-readable key accepted")
	}
	if err := os.Chmod(f.keyPath, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB.Exec("INSERT INTO inventory_cache(host_id,snapshot_json,observed_at) VALUES (?, '{}',?)", f.host.ID, storage.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Configure(ctx, f.host.ID, f.keyPath, f.publicPath, f.record.Fingerprint, 22); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("old key not removed on rotation")
	}
	var count int
	if err := f.store.DB.QueryRow("SELECT COUNT(*) FROM inventory_cache").Scan(&count); err != nil || count != 0 {
		t.Fatal("old inventory retained after rotation")
	}
	newRecord, err := f.store.Load(ctx, f.host.ID)
	if err != nil {
		t.Fatal(err)
	}
	newPath, _ := f.store.path(newRecord.KeyRef)
	if err := f.store.Revoke(ctx, f.host.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Load(ctx, f.host.ID); err != ErrRequired {
		t.Fatal("revoked key retained")
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatal("key not removed on revoke")
	}
	var stored string
	if err := f.store.DB.QueryRow("SELECT result FROM audit_events WHERE action='ssh.revoke'").Scan(&stored); err != nil || stored != "success" {
		t.Fatal("revoke not audited")
	}
}

func TestBoundedWriterCannotBypassLimitWithIOCopy(t *testing.T) {
	b := &bounded{max: 8}
	if _, err := io.Copy(b, strings.NewReader(strings.Repeat("x", 32))); err == nil || !b.exceeded || len(b.Bytes()) > 8 {
		t.Fatal("io.Copy bypassed output limit")
	}
}

func TestOnlyOneObservationPerHostAndCancellation(t *testing.T) {
	f := setup(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), func(command string) reply { once.Do(func() { close(started); <-release }); return normal(command) })
	m := manager(f, address)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.Snapshot(ctx, f.host, f.record); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("SSH command did not start")
	}
	if _, err := m.Snapshot(context.Background(), f.host, f.record); Code(err) != "busy" {
		close(release)
		t.Fatal("parallel observation on same host accepted")
	}
	cancel()
	select {
	case err := <-done:
		if Code(err) != "timeout" {
			close(release)
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("cancellation did not close transport")
	}
	close(release)
	if _, err := m.Snapshot(context.Background(), f.host, f.record); err != nil {
		t.Fatalf("host slot not released: %v", err)
	}
}

func TestCredentialConfigureFailureLeavesExistingKeyIntact(t *testing.T) {
	f := setup(t)
	if _, err := f.store.DB.Exec("CREATE TRIGGER fail_config BEFORE INSERT ON audit_events WHEN NEW.action='ssh.configure' BEGIN SELECT RAISE(FAIL,'blocked'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Configure(context.Background(), f.host.ID, f.keyPath, f.publicPath, f.record.Fingerprint, 22); err == nil {
		t.Fatal("audit failure ignored")
	}
	r, err := f.store.Load(context.Background(), f.host.ID)
	if err != nil || r.KeyRef != f.record.KeyRef {
		t.Fatal("credential changed without audit")
	}
	files, err := os.ReadDir(filepath.Join(f.store.DataDir, "secrets", "ssh"))
	if err != nil || len(files) != 1 {
		t.Fatal("uncommitted secret file was not removed")
	}
}

func TestAllResolvedAddressesValidatedBeforeFallbackDial(t *testing.T) {
	f := setup(t)
	address := server(t, f.hostSigner, f.clientSigner.PublicKey(), normal)
	m := manager(f, address)
	lookups, dials := 0, 0
	m.resolve = func(context.Context, string) ([]netip.Addr, error) {
		lookups++
		return []netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::1"), netip.MustParseAddr("100.64.1.2")}, nil
	}
	m.dial = func(ctx context.Context, network, target string) (net.Conn, error) {
		dials++
		if dials == 1 {
			return nil, fmt.Errorf("unreachable IPv6")
		}
		if target != "100.64.1.2:22" {
			return nil, fmt.Errorf("unexpected address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	if _, err := m.Snapshot(context.Background(), f.host, f.record); err != nil || lookups != 1 || dials != 2 {
		t.Fatalf("validated fallback: %v lookups=%d dials=%d", err, lookups, dials)
	}
	m.resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("100.64.1.2"), netip.MustParseAddr("192.168.1.2")}, nil
	}
	dials = 0
	if _, err := m.Snapshot(context.Background(), f.host, f.record); Code(err) != "address_not_tailnet" || dials != 0 {
		t.Fatal("mixed public/private DNS resolution was dialed")
	}
}
