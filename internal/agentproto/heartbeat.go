package agentproto

import (
	"regexp"
	"time"

	"github.com/coder/websocket"
)

const HeartbeatType = "heartbeat"
const HeartbeatAckType = "heartbeat.ack"
const DuplicateConnection websocket.StatusCode = 4001
const MinHeartbeatSeconds = 5
const MaxHeartbeatSeconds = 60
const DefaultHeartbeatSeconds = 15
const MissedHeartbeats = 3

var instancePattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ValidInstanceID(value string) bool { return instancePattern.MatchString(value) }

func HeartbeatInterval(seconds int) (time.Duration, bool) {
	if seconds < MinHeartbeatSeconds || seconds > MaxHeartbeatSeconds {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

type Heartbeat struct {
	Type       string `json:"type"`
	AgentID    string `json:"agentId"`
	Generation int64  `json:"generation"`
	Sequence   uint64 `json:"sequence"`
	Tmux       string `json:"tmuxVersion"`
}

type HeartbeatAck struct {
	Type       string `json:"type"`
	AgentID    string `json:"agentId"`
	Generation int64  `json:"generation"`
	Sequence   uint64 `json:"sequence"`
}
