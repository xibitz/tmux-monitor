package agentproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"regexp"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

const Version = 2
const Subprotocol = "tmux-agent.v2"
const LegacyVersion = 1
const LegacySubprotocol = "tmux-agent.v1"
const HandshakeTimeout = 5 * time.Second
const SnapshotTimeout = 8 * time.Second
const ControlLimit = 4096
const SnapshotFrameLimit = (tmux.MaxOutput+2)/3*4 + ControlLimit

const Snapshot = "inventory.snapshot"
const SnapshotReply = "inventory.snapshot.result"

var namePattern = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$`)
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+[a-z]?$`)
var secretPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var hostPattern = regexp.MustCompile(`^h_[a-f0-9]{32}$`)
var agentPattern = regexp.MustCompile(`^a_[a-f0-9]{32}$`)
var requestPattern = regexp.MustCompile(`^rpc_[a-f0-9]{32}$`)
var tailnet4 = netip.MustParsePrefix("100.64.0.0/10")
var tailnet6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

func ValidName(value string) bool { return namePattern.MatchString(value) }
func ValidTmuxVersion(value string) bool {
	return len(value) <= 16 && versionPattern.MatchString(value)
}
func ValidSecret(value string) bool    { return secretPattern.MatchString(value) }
func ValidHostID(value string) bool    { return hostPattern.MatchString(value) }
func ValidAgentID(value string) bool   { return agentPattern.MatchString(value) }
func ValidRequestID(value string) bool { return requestPattern.MatchString(value) }

func TailnetIP(value string) (string, bool) {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return "", false
	}
	ip = ip.Unmap()
	if ip.Zone() != "" || !(tailnet4.Contains(ip) || tailnet6.Contains(ip)) {
		return "", false
	}
	return ip.String(), true
}

type EnrollRequest struct {
	HostID string `json:"hostId"`
	OSUser string `json:"osUser"`
	Socket string `json:"socket"`
}

type EnrollResult struct {
	AgentID    string `json:"agentId"`
	Credential string `json:"credential"`
}

type Hello struct {
	Type               string `json:"type"`
	Version            int    `json:"version"`
	AgentID            string `json:"agentId"`
	OS                 string `json:"os"`
	OSUser             string `json:"osUser"`
	Socket             string `json:"socket"`
	Tmux               string `json:"tmuxVersion"`
	Inventory          bool   `json:"inventory"`
	ControlEvents      bool   `json:"controlEvents,omitempty"`
	CodingHooks        bool   `json:"codingHooks,omitempty"`
	Terminal           bool   `json:"terminal,omitempty"`
	InstanceID         string `json:"instanceId,omitempty"`
	PreviousGeneration int64  `json:"previousGeneration,omitempty"`
}

type HelloAck struct {
	Type             string `json:"type"`
	Version          int    `json:"version"`
	AgentID          string `json:"agentId"`
	Generation       int64  `json:"generation"`
	HeartbeatSeconds int    `json:"heartbeatSeconds,omitempty"`
	ControlEvents    bool   `json:"controlEvents,omitempty"`
	CodingHooks      bool   `json:"codingHooks,omitempty"`
}

type Request struct {
	Type       string    `json:"type"`
	RequestID  string    `json:"requestId"`
	AgentID    string    `json:"agentId"`
	Generation int64     `json:"generation"`
	Deadline   time.Time `json:"deadline"`
}

type SnapshotResult struct {
	Type       string `json:"type"`
	RequestID  string `json:"requestId"`
	AgentID    string `json:"agentId"`
	Generation int64  `json:"generation"`
	Status     string `json:"status"`
	Version    string `json:"tmuxVersion,omitempty"`
	Output     []byte `json:"output,omitempty"`
}

func Decode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON message")
	}
	return nil
}

func Read(ctx context.Context, conn *websocket.Conn, dst any) error {
	kind, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	if kind != websocket.MessageText {
		return errors.New("expected text message")
	}
	return Decode(data, dst)
}

func Write(ctx context.Context, conn *websocket.Conn, src any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
