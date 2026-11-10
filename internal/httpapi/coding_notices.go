package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/storage"
)

type codingNotice struct {
	ID         int64  `json:"id"`
	HostID     string `json:"hostId"`
	HostLabel  string `json:"hostLabel"`
	Source     string `json:"source"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
	Epoch      string `json:"epoch,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	PaneID     string `json:"paneId,omitempty"`
	OccurredAt string `json:"occurredAt"`
}

func (s *Server) recordCodingNotice(p *agentPeer, event agentproto.CodingMessage) {
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Second)
	defer cancel()
	if !p.live() {
		return
	}
	confidence := "unknown"
	p.mu.Lock()
	if event.Epoch != "" && p.inventoryReady && p.inventoryEpoch == event.Epoch &&
		time.Since(p.observationTime) <= agentSnapshotFreshness {
		confidence = "verified_pane"
	} else {
		event.Epoch, event.SessionID, event.PaneID = "", "", ""
	}
	p.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO coding_notices
		(host_id,source,kind,confidence,epoch,session_id,pane_id,dedupe,occurred_at)
		SELECT h.id,?,?,?,?,?,?,?,? FROM agent_identities a JOIN hosts h ON h.id=a.host_id
		WHERE a.id=? AND a.host_id=? AND a.generation=? AND a.revoked_at IS NULL
			AND h.transport='agent' AND h.socket_name=? AND h.os_user=?
		ON CONFLICT(host_id,dedupe) DO NOTHING`, event.Source, event.Kind, confidence, event.Epoch,
		event.SessionID, event.PaneID, event.Dedupe, storage.Now(), p.id, p.hostID, p.generation, p.socket, p.osUser)
	if err != nil {
		return
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM coding_notices WHERE host_id=? AND id <=
		(SELECT id FROM coding_notices WHERE host_id=? ORDER BY id DESC LIMIT 1 OFFSET 200)`, p.hostID, p.hostID); err != nil {
		return
	}
	if tx.Commit() == nil && s.cfg.WebPush && event.Kind != "unknown" {
		s.queueWebPush(p.hostID)
	}
}

func (s *Server) codingNotices(w http.ResponseWriter, r *http.Request) {
	notices := []codingNotice{}
	if s.cfg.CodingHooks {
		actor := user(r)
		rows, err := s.db.QueryContext(r.Context(), `SELECT n.id,n.host_id,h.label,n.source,n.kind,n.confidence,
			n.epoch,n.session_id,n.pane_id,n.occurred_at
			FROM coding_notices n JOIN hosts h ON h.id=n.host_id
			WHERE `+codingAccessSQL+` ORDER BY n.id DESC LIMIT 50`, actor.IsAdmin, actor.ID)
		if err != nil {
			s.internal(w, r)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var n codingNotice
			if err := rows.Scan(&n.ID, &n.HostID, &n.HostLabel, &n.Source, &n.Kind, &n.Confidence,
				&n.Epoch, &n.SessionID, &n.PaneID, &n.OccurredAt); err != nil {
				s.internal(w, r)
				return
			}
			notices = append(notices, n)
		}
		if rows.Err() != nil {
			s.internal(w, r)
			return
		}
	}
	s.json(w, http.StatusOK, map[string]any{"notices": notices, "enabled": s.cfg.CodingHooks})
}

const codingAccessSQL = `(? OR (SELECT count(*) FROM host_access
	WHERE host_id=h.id AND user_id=? AND action IN ('host:read','tmux:read'))=2)`

func (s *Server) readPushEndpoint(ctx context.Context, hostID, id string) ([]byte, error) {
	var endpoint []byte
	// Re-check the account, current ACL and subscription immediately before delivery.
	row := s.db.QueryRowContext(ctx, `UPDATE push_subscriptions SET last_sent_at=? WHERE id=?
		AND last_sent_at<? AND EXISTS (
			SELECT 1 FROM users u JOIN hosts h ON h.id=? WHERE u.id=push_subscriptions.user_id
				AND u.disabled_at IS NULL AND (u.is_admin=1 OR (SELECT count(*) FROM host_access
				WHERE host_id=h.id AND user_id=u.id AND action IN ('host:read','tmux:read'))=2)
		) RETURNING encrypted_endpoint`, storage.Now(), id, storage.Timestamp(time.Now().Add(-30*time.Second)), hostID)
	err := row.Scan(&endpoint)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return endpoint, err
}
