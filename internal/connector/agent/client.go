package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
)

type Binding struct {
	Hub        string `json:"hub"`
	HostID     string `json:"hostId"`
	AgentID    string `json:"agentId"`
	OSUser     string `json:"osUser"`
	Socket     string `json:"socket"`
	Credential string `json:"credential"`
}

func ValidateHub(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" ||
		!strings.HasSuffix(strings.ToLower(u.Hostname()), ".ts.net") ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.Opaque != "" {
		return nil, errors.New("hub must be an HTTPS Tailscale Serve origin")
	}
	u.Path = ""
	return u, nil
}

func client() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   agentproto.HandshakeTimeout,
			ResponseHeaderTimeout: agentproto.HandshakeTimeout,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func Enroll(ctx context.Context, hub, token string, profile agentproto.EnrollRequest) (Binding, error) {
	u, err := ValidateHub(hub)
	if err != nil {
		return Binding{}, err
	}
	if !agentproto.ValidSecret(token) || !agentproto.ValidHostID(profile.HostID) ||
		!agentproto.ValidName(profile.OSUser) || !agentproto.ValidName(profile.Socket) {
		return Binding{}, errors.New("invalid enrollment token or profile")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := json.Marshal(profile)
	if err != nil {
		return Binding{}, err
	}
	origin := u.String()
	u.Path = "/api/v1/agents/enroll"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return Binding{}, errors.New("cannot construct enrollment request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	c := client()
	defer c.CloseIdleConnections()
	res, err := c.Do(req)
	if err != nil {
		return Binding{}, errors.New("enrollment could not reach the hub over HTTPS")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return Binding{}, errors.New("enrollment rejected; check token, profile and Tailnet IP")
	}
	reply, err := io.ReadAll(io.LimitReader(res.Body, agentproto.ControlLimit+1))
	var result agentproto.EnrollResult
	if err != nil || len(reply) > agentproto.ControlLimit || agentproto.Decode(reply, &result) != nil ||
		!agentproto.ValidAgentID(result.AgentID) || !agentproto.ValidSecret(result.Credential) {
		return Binding{}, errors.New("invalid enrollment response")
	}
	return Binding{Hub: origin, HostID: profile.HostID, AgentID: result.AgentID, OSUser: profile.OSUser, Socket: profile.Socket, Credential: result.Credential}, nil
}

func (b Binding) Run(ctx context.Context, osName string) error {
	u, err := ValidateHub(b.Hub)
	if err != nil {
		return err
	}
	u.Scheme, u.Path = "wss", "/ws/v1/agent"
	probe, cancelProbe := context.WithTimeout(ctx, 2*time.Second)
	version, _ := tmuxVersion(probe)
	cancelProbe()
	dial, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	c := client()
	defer c.CloseIdleConnections()
	conn, _, err := websocket.Dial(dial, u.String(), &websocket.DialOptions{
		HTTPClient: c, HTTPHeader: http.Header{"Authorization": {"Bearer " + b.Credential}},
		Subprotocols: []string{agentproto.Subprotocol}, CompressionMode: websocket.CompressionDisabled,
	})
	cancelDial()
	if err != nil {
		return errors.New("Agent WebSocket rejected or unreachable")
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != agentproto.Subprotocol {
		return errors.New("Agent protocol mismatch")
	}
	conn.SetReadLimit(agentproto.ControlLimit)
	helloCtx, cancelHello := context.WithTimeout(ctx, agentproto.HandshakeTimeout)
	hello := agentproto.Hello{Type: "hello", Version: agentproto.Version, AgentID: b.AgentID, OS: osName,
		OSUser: b.OSUser, Socket: b.Socket, Tmux: version, Inventory: true}
	err = agentproto.Write(helloCtx, conn, hello)
	var ack agentproto.HelloAck
	if err == nil {
		err = agentproto.Read(helloCtx, conn, &ack)
	}
	cancelHello()
	if err != nil || ack.Type != "hello.ack" || ack.Version != agentproto.Version ||
		ack.AgentID != b.AgentID || ack.Generation < 1 {
		return errors.New("Agent hello was rejected")
	}
	for {
		var req agentproto.Request
		if err := agentproto.Read(ctx, conn, &req); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("Agent connection closed")
		}
		now := time.Now()
		if req.Type != agentproto.Snapshot || req.AgentID != b.AgentID || req.Generation != ack.Generation ||
			!agentproto.ValidRequestID(req.RequestID) || req.Deadline.IsZero() ||
			req.Deadline.After(now.Add(agentproto.SnapshotTimeout+5*time.Second)) {
			return errors.New("invalid Agent RPC")
		}
		// Absolute time limits caller intent. The local context also bounds execution.
		operation, cancel := context.WithDeadline(ctx, req.Deadline)
		result := snapshot(operation, b.Socket)
		cancel()
		result.Type, result.RequestID, result.AgentID, result.Generation = agentproto.SnapshotReply, req.RequestID, b.AgentID, ack.Generation
		write, cancel := context.WithTimeout(ctx, agentproto.HandshakeTimeout)
		err := agentproto.Write(write, conn, result)
		cancel()
		if err != nil {
			return errors.New("Agent could not return the snapshot")
		}
	}
}
