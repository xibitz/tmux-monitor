package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	mathrand "math/rand/v2"
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

func (b Binding) Run(ctx context.Context, osName string, enableTerminal bool) error {
	u, err := ValidateHub(b.Hub)
	if err != nil {
		return err
	}
	u.Scheme, u.Path = "wss", "/ws/v1/agent"
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return errors.New("cannot create Agent instance ID")
	}
	instance := hex.EncodeToString(seed[:])
	var generation int64
	c := client()
	defer c.CloseIdleConnections()
	backoff := time.Second
	for ctx.Err() == nil {
		uptime, err := b.connect(ctx, c, u.String(), osName, instance, &generation, enableTerminal)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errAgentRejected) {
			return err
		}
		if uptime >= time.Minute {
			backoff = time.Second
		}
		delay := backoff/2 + mathrand.N(backoff/2)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	return nil
}

var errAgentRejected = errors.New("Agent cannot reconnect")

func (b Binding) connect(ctx context.Context, c *http.Client, endpoint, osName, instance string, generation *int64, enableTerminal bool) (time.Duration, error) {
	probe, cancelProbe := context.WithTimeout(ctx, 2*time.Second)
	version, _ := tmuxVersion(probe)
	cancelProbe()
	dial, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	conn, response, err := websocket.Dial(dial, endpoint, &websocket.DialOptions{
		HTTPClient: c, HTTPHeader: http.Header{"Authorization": {"Bearer " + b.Credential}},
		Subprotocols: []string{agentproto.Subprotocol}, CompressionMode: websocket.CompressionDisabled,
	})
	cancelDial()
	if err != nil {
		if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
			return 0, errors.Join(errAgentRejected, errors.New("credential or Tailnet IP rejected"))
		}
		return 0, errors.New("Agent WebSocket unreachable")
	}
	defer conn.CloseNow()
	if conn.Subprotocol() != agentproto.Subprotocol {
		return 0, errors.Join(errAgentRejected, errors.New("upgrade the hub to the current Agent protocol"))
	}
	conn.SetReadLimit(agentproto.ControlLimit)
	helloCtx, cancelHello := context.WithTimeout(ctx, agentproto.HandshakeTimeout)
	hello := agentproto.Hello{Type: "hello", Version: agentproto.Version, AgentID: b.AgentID, OS: osName,
		OSUser: b.OSUser, Socket: b.Socket, Tmux: version, Inventory: true, Terminal: enableTerminal,
		InstanceID: instance, PreviousGeneration: *generation}
	err = agentproto.Write(helloCtx, conn, hello)
	var ack agentproto.HelloAck
	if err == nil {
		err = agentproto.Read(helloCtx, conn, &ack)
	}
	cancelHello()
	if websocket.CloseStatus(err) == agentproto.DuplicateConnection {
		return 0, errors.Join(errAgentRejected, errors.New("another Agent instance owns this profile"))
	}
	interval, ok := agentproto.HeartbeatInterval(ack.HeartbeatSeconds)
	if err != nil || ack.Type != "hello.ack" || ack.Version != agentproto.Version ||
		ack.AgentID != b.AgentID || ack.Generation <= *generation || !ok {
		return 0, errors.New("Agent hello was rejected")
	}
	*generation = ack.Generation
	started := time.Now()
	err = b.controlLoop(ctx, conn, c, ack.Generation, interval, enableTerminal)
	return time.Since(started), err
}
