package agentproto

import (
	"regexp"

	"github.com/xibitz/tmux-monitor/internal/tmux"
)

const InventoryChanged = "inventory.changed"
const CodingEvent = "coding.event"

type InventoryEvent struct {
	Type       string `json:"type"`
	AgentID    string `json:"agentId"`
	Generation int64  `json:"generation"`
	Sequence   uint64 `json:"sequence"`
}

type CodingNotice struct {
	Schema int    `json:"schema"`
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Dedupe string `json:"dedupe"`
}

type CodingMessage struct {
	Type       string `json:"type"`
	AgentID    string `json:"agentId"`
	Generation int64  `json:"generation"`
	CodingNotice
	Epoch     string `json:"epoch,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	PaneID    string `json:"paneId,omitempty"`
}

func (m CodingMessage) Valid() bool {
	return m.CodingNotice.Valid() && ((m.Epoch == "" && m.SessionID == "" && m.PaneID == "") ||
		(epochPattern.MatchString(m.Epoch) && tmux.ValidSessionID(m.SessionID) && tmux.ValidPaneID(m.PaneID)))
}

var dedupePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (n CodingNotice) Valid() bool {
	if n.Schema != 1 || !dedupePattern.MatchString(n.Dedupe) {
		return false
	}
	switch n.Source {
	case "codex":
		return n.Kind == "turn_completed" || n.Kind == "unknown"
	case "claude":
		return n.Kind == "turn_completed" || n.Kind == "attention_required" || n.Kind == "error" || n.Kind == "unknown"
	default:
		return false
	}
}
