package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type localHook struct {
	agentproto.CodingNotice
	Socket string `json:"socket,omitempty"`
	PID    string `json:"pid,omitempty"`
	Pane   string `json:"pane,omitempty"`
}

func ValidateHookPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 100 {
		return errors.New("hook socket must be a clean absolute path of at most 100 bytes")
	}
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("hook socket directory must be private (0700)")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return errors.New("hook socket directory must belong to the current user")
	}
	return nil
}

func NotifyHook(parent context.Context, args []string, input io.Reader) error {
	flags := flag.NewFlagSet("notify", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // CLI parse errors can contain the raw Codex argument.
	socket := flags.String("socket", "", "private Agent socket")
	source := flags.String("source", "", "codex or claude")
	if flags.Parse(args) != nil || (*source != "codex" && *source != "claude") ||
		(*source == "codex" && flags.NArg() != 1) || (*source == "claude" && flags.NArg() != 0) {
		return errors.New("usage: agent notify --socket PATH --source codex|claude")
	}
	if ValidateHookPath(*socket) != nil {
		return nil
	}
	const inputLimit = 256 << 10
	var raw []byte
	if *source == "codex" {
		if len(flags.Arg(0)) > inputLimit {
			return nil
		}
		raw = []byte(flags.Arg(0))
	} else {
		raw, _ = io.ReadAll(io.LimitReader(input, inputLimit+1))
	}
	defer clear(raw)
	if len(raw) > inputLimit {
		return nil
	}
	var event struct {
		Type    string `json:"type"`
		Thread  string `json:"thread-id"`
		Turn    string `json:"turn-id"`
		Hook    string `json:"hook_event_name"`
		Session string `json:"session_id"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return nil
	}
	notice := localHook{CodingNotice: agentproto.CodingNotice{Schema: 1, Source: *source, Kind: "unknown"}}
	if *source == "codex" && event.Type == "agent-turn-complete" {
		notice.Kind = "turn_completed"
	}
	if *source == "claude" {
		switch event.Hook {
		case "Notification":
			notice.Kind = "attention_required"
		case "Stop":
			notice.Kind = "turn_completed"
		case "StopFailure":
			notice.Kind = "error"
		}
	}
	tmuxContext := os.Getenv("TMUX")
	pidIndex := strings.LastIndexByte(tmuxContext, ',')
	if pidIndex >= 0 {
		tmuxContext = tmuxContext[:pidIndex]
		pidIndex = strings.LastIndexByte(tmuxContext, ',')
	}
	pane := os.Getenv("TMUX_PANE")
	if pidIndex > 0 && pidIndex <= 512 && len(tmuxContext)-pidIndex <= 21 && tmux.ValidPaneID(pane) {
		pid := tmuxContext[pidIndex+1:]
		if _, err := strconv.ParseUint(pid, 10, 64); err == nil {
			notice.Socket, notice.PID, notice.Pane = tmuxContext[:pidIndex], pid, pane
		}
	}
	bucket := strconv.FormatInt(time.Now().Unix()/5, 10)
	fields := []string{notice.Source, notice.Kind, notice.Pane, event.Session, bucket}
	if *source == "codex" && event.Thread != "" && event.Turn != "" {
		fields = []string{notice.Source, notice.Kind, event.Thread, event.Turn}
	}
	digest := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	notice.Dedupe = hex.EncodeToString(digest[:])

	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", *socket)
	if err != nil {
		return nil // A disconnected hub must not interfere with a coding turn.
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	_ = json.NewEncoder(conn).Encode(notice)
	return nil
}

func (b Binding) listenHooks(ctx context.Context, hub *websocket.Conn, generation int64, path string) {
	if ValidateHookPath(path) != nil {
		return
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return
	}
	defer listener.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var last time.Time
	for ctx.Err() == nil {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		raw, err := io.ReadAll(io.LimitReader(conn, agentproto.ControlLimit+1))
		_ = conn.Close()
		var local localHook
		if err != nil || len(raw) > agentproto.ControlLimit || agentproto.Decode(raw, &local) != nil ||
			!local.Valid() || time.Since(last) < 500*time.Millisecond {
			continue
		}
		last = time.Now()
		event := agentproto.CodingMessage{Type: agentproto.CodingEvent, AgentID: b.AgentID, Generation: generation, CodingNotice: local.CodingNotice}
		if len(local.Socket) <= 512 && len(local.PID) <= 20 && tmux.ValidPaneID(local.Pane) {
			probe, cancel := context.WithTimeout(ctx, 2*time.Second)
			observed := snapshot(probe, b.Socket)
			cancel()
			if observed.Status == "ok" {
				if inv, err := tmux.Parse(b.HostID, b.Socket, observed.Version, observed.Output); err == nil {
					event.Epoch, event.SessionID, event.PaneID = tmux.CodingPane(inv, observed.Output, local.Socket, local.PID, local.Pane)
				}
			}
		}
		write, cancel := context.WithTimeout(ctx, agentproto.HandshakeTimeout)
		err = agentproto.Write(write, hub, event)
		cancel()
		if err != nil {
			return
		}
	}
}
