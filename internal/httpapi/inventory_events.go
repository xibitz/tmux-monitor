package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/xibitz/tmux-monitor/internal/agentproto"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

type inventoryWatcher interface {
	WatchInventory(context.Context, sshconnector.Host, sshconnector.Record, func()) error
}

func (s *Server) inventoryEvents(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("hostId")
	if !s.cfg.ControlEvents || !agentproto.ValidHostID(hostID) {
		s.fail(w, r, http.StatusNotFound, "not_found", "Stream inventaris tidak tersedia.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	stop := context.AfterFunc(s.terminalRoot, cancel)
	defer stop()
	h, err := s.viewerHost(ctx, user(r), hostID, "")
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "not_found", "Inventaris host tidak tersedia.")
		return
	}
	select {
	case s.eventStreams <- struct{}{}:
		defer func() { <-s.eventStreams }()
	default:
		s.fail(w, r, http.StatusTooManyRequests, "busy", "Stream inventaris sedang penuh.")
		return
	}
	var record sshconnector.Record
	var watcher inventoryWatcher
	if h.Transport == "ssh" {
		var ok bool
		watcher, ok = s.ssh.(inventoryWatcher)
		record, err = s.sshViewerCredentials(ctx, h)
		if !ok || err != nil {
			s.fail(w, r, http.StatusNotFound, "not_found", "Stream SSH tidak tersedia.")
			return
		}
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  []string{strings.TrimPrefix(strings.TrimPrefix(s.cfg.Origin, "https://"), "http://")},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1024)
	ctx = conn.CloseRead(ctx)
	dirty := make(chan struct{}, 1)
	watchClosed := make(chan struct{})
	if watcher != nil {
		watchCtx, stopWatch := context.WithCancel(ctx)
		go func() {
			defer close(watchClosed)
			_ = watcher.WatchInventory(watchCtx, sshconnector.Host{ID: h.ID, Address: h.TailnetAddress, User: h.OSUser, Socket: h.SocketName}, record, func() {
				select {
				case dirty <- struct{}{}:
				default:
				}
			})
		}()
		defer func() { stopWatch(); <-watchClosed }()
	}
	stream := storage.ID("stream_")
	var sequence uint64
	emit := func(kind string) error {
		sequence++
		write, stopWrite := context.WithTimeout(ctx, 3*time.Second)
		defer stopWrite()
		return agentproto.Write(write, conn, map[string]any{"type": kind, "stream": stream, "sequence": sequence})
	}
	if emit("resync_required") != nil {
		return
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var peer *agentPeer
	var last uint64
	var ticks int
	for {
		select {
		case <-ctx.Done():
			return
		case <-watchClosed:
			return // The browser falls back to GET while reconnecting.
		case <-tick.C:
			ticks++
			kind := ""
			if h.Transport == "agent" {
				current := s.agents.peer(h.ID)
				var next uint64
				if current != nil && current.live() && current.controlEvents {
					next = current.eventSequence.Load()
				} else {
					current = nil
				}
				if current != peer || (next > last && next-last > 1) {
					kind = "resync_required"
				} else if next != last {
					kind = "inventory.changed"
				}
				peer, last = current, next
			} else {
				select {
				case <-dirty:
					kind = "inventory.changed"
				default:
				}
			}
			if ticks%20 == 0 {
				kind = "resync_required" // Includes transport/capability fallback.
			}
			if kind == "" && ticks%5 != 0 {
				continue
			}
			actor, authErr := identity.Authenticate(ctx, s.db, token(r))
			if authErr != nil || actor.ID != user(r).ID {
				return
			}
			current, hostErr := s.viewerHost(ctx, actor, h.ID, h.Transport)
			if hostErr != nil ||
				current.OSUser != h.OSUser || current.SocketName != h.SocketName || current.TailnetAddress != h.TailnetAddress {
				return
			}
			if watcher != nil {
				key, err := s.sshViewerCredentials(ctx, current)
				if err != nil || key.KeyRef != record.KeyRef {
					return
				}
			}
			if kind != "" && emit(kind) != nil {
				return
			}
		}
	}
}
