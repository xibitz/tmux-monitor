package sshconnector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xibitz/tmux-monitor/internal/tmux"
	"golang.org/x/crypto/ssh"
)

type Host struct{ ID, Address, User, Socket string }
type Failure struct{ Code string }

func (f *Failure) Error() string { return f.Code }
func fail(code string) error     { return &Failure{Code: code} }
func Code(err error) string {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	if errors.Is(err, ErrRequired) {
		return "credentials_required"
	}
	if errors.Is(err, ErrCredentials) {
		return "credentials_invalid"
	}
	return "connection_failed"
}
func State(code string) string {
	switch code {
	case "tmux_missing", "inventory_invalid", "inventory_changed", "output_limit", "os_user_mismatch", "command_failed":
		return "DEGRADED"
	}
	return "UNREACHABLE"
}

type Manager struct {
	Store   Store
	Allowed map[string]bool
	Timeout time.Duration
	mu      sync.Mutex
	busy    map[string]bool
	gate    chan struct{}
	resolve func(context.Context, string) ([]netip.Addr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
}

func New(store Store, allowed map[string]bool) *Manager {
	m := &Manager{Store: store, Allowed: allowed, Timeout: 10 * time.Second, busy: map[string]bool{}, gate: make(chan struct{}, 4)}
	m.resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	m.dial = (&net.Dialer{}).DialContext
	return m
}

func (m *Manager) Snapshot(ctx context.Context, h Host, record Record) (tmux.Inventory, error) {
	m.mu.Lock()
	if m.busy[h.ID] {
		m.mu.Unlock()
		return tmux.Inventory{}, fail("busy")
	}
	select {
	case m.gate <- struct{}{}:
	default:
		m.mu.Unlock()
		return tmux.Inventory{}, fail("busy")
	}
	m.busy[h.ID] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.busy, h.ID); m.mu.Unlock(); <-m.gate }()
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	if !m.Allowed[h.Address] {
		return tmux.Inventory{}, fail("address_not_allowed")
	}
	command, err := tmux.SnapshotCommand(h.Socket)
	if err != nil {
		return tmux.Inventory{}, fail("inventory_invalid")
	}
	signer, hostKey, err := m.Store.Key(record)
	if err != nil {
		return tmux.Inventory{}, err
	}
	addresses, err := m.resolve(ctx, h.Address)
	if err != nil {
		return tmux.Inventory{}, networkError(ctx, err)
	}
	if len(addresses) == 0 || len(addresses) > 16 {
		return tmux.Inventory{}, fail("connection_failed")
	}
	for _, ip := range addresses {
		if !tailnetIP(ip) {
			return tmux.Inventory{}, fail("address_not_tailnet")
		}
	}
	// Dial the validated IP, not the DNS name again (avoids DNS rebinding).
	var conn net.Conn
	for _, ip := range addresses {
		attempt, cancelAttempt := context.WithTimeout(ctx, 3*time.Second)
		conn, err = m.dial(attempt, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(record.Port)))
		cancelAttempt()
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return tmux.Inventory{}, networkError(ctx, err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return tmux.Inventory{}, fail("connection_failed")
	}
	var mismatch atomic.Bool
	check := ssh.FixedHostKey(hostKey)
	config := &ssh.ClientConfig{User: h.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyAlgorithms: hostAlgorithms(hostKey), HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := check(hostname, remote, key); err != nil {
			mismatch.Store(true)
			return fail("host_key_mismatch")
		}
		return nil
	}}
	cc, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(h.Address, strconv.Itoa(record.Port)), config)
	if err != nil {
		if mismatch.Load() {
			return tmux.Inventory{}, fail("host_key_mismatch")
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return tmux.Inventory{}, fail("authentication_failed")
		}
		return tmux.Inventory{}, networkError(ctx, err)
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	// Cancel closes the transport even when the peer stops responding mid-command.
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	identity, _, err := run(ctx, client, "LC_ALL=C id -un", 1024)
	if err != nil {
		return tmux.Inventory{}, err
	}
	if strings.TrimSpace(string(identity)) != h.User {
		return tmux.Inventory{}, fail("os_user_mismatch")
	}
	versionBytes, _, err := run(ctx, client, "LC_ALL=C tmux -V", 1024)
	if err != nil {
		return tmux.Inventory{}, versionError(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	if !regexp.MustCompile(`^tmux [0-9]+\.[0-9]+[a-z]?$`).MatchString(version) {
		return tmux.Inventory{}, fail("tmux_missing")
	}
	version = strings.TrimPrefix(version, "tmux ")
	output, stderr, err := run(ctx, client, command, tmux.MaxOutput)
	if err != nil && Code(err) == "command_failed" && strings.TrimSpace(string(stderr)) == "no current target" {
		emptyCommand, commandErr := tmux.EmptySnapshotCommand(h.Socket)
		if commandErr != nil {
			return tmux.Inventory{}, fail("inventory_invalid")
		}
		output, stderr, err = run(ctx, client, emptyCommand, tmux.MaxOutput)
	}
	if err != nil {
		if code := Code(err); code == "command_failed" && noServer(stderr) {
			return tmux.Empty(h.ID, h.Socket, version), nil
		}
		return tmux.Inventory{}, err
	}
	inv, err := tmux.Parse(h.ID, h.Socket, version, output)
	if err != nil {
		if errors.Is(err, tmux.ErrChanged) {
			return inv, fail("inventory_changed")
		}
		return inv, fail("inventory_invalid")
	}
	return inv, nil
}

func versionError(err error) error {
	if Code(err) == "command_failed" {
		return fail("tmux_missing")
	}
	return err
}
func noServer(stderr []byte) bool {
	text := strings.TrimSpace(string(stderr))
	return strings.HasPrefix(text, "no server running on ") || (strings.HasPrefix(text, "error connecting to ") && strings.Contains(text, "(No such file or directory)"))
}
func networkError(ctx context.Context, err error) error {
	var n net.Error
	if ctx.Err() != nil || errors.As(err, &n) && n.Timeout() {
		return fail("timeout")
	}
	return fail("connection_failed")
}

var tailnet4 = netip.MustParsePrefix("100.64.0.0/10")
var tailnet6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

func tailnetIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.Zone() == "" && (tailnet4.Contains(ip) || tailnet6.Contains(ip))
}

type bounded struct {
	buf      bytes.Buffer
	max      int
	exceeded bool
}

func (b *bounded) Write(p []byte) (int, error) {
	if len(p) > b.max-b.buf.Len() {
		b.exceeded = true
		return 0, io.ErrShortBuffer
	}
	return b.buf.Write(p)
}

func (b *bounded) Bytes() []byte { return b.buf.Bytes() }
func run(ctx context.Context, client *ssh.Client, command string, max int) ([]byte, []byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, nil, networkError(ctx, err)
	}
	defer session.Close()
	out, stderr := &bounded{max: max}, &bounded{max: 16 * 1024}
	session.Stdout = out
	session.Stderr = stderr
	err = session.Run(command)
	if out.exceeded || stderr.exceeded {
		return nil, nil, fail("output_limit")
	}
	if ctx.Err() != nil {
		return nil, nil, fail("timeout")
	}
	if err != nil {
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			return nil, stderr.Bytes(), fail("command_failed")
		}
		return nil, nil, networkError(ctx, err)
	}
	return out.Bytes(), stderr.Bytes(), nil
}
