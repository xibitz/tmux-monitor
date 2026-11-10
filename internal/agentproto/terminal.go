package agentproto

import (
	"encoding/binary"
	"regexp"
	"time"
)

const TerminalProtocol = "tmux-agent-terminal.v1"
const TerminalOpen = "terminal.open"
const TerminalReady = "terminal.ready"
const TerminalDetach = "terminal.detach"
const TerminalClosed = "terminal.closed"
const TerminalTicketHeader = "X-Agent-Stream-Ticket"
const TerminalSetupTimeout = 10 * time.Second
const TerminalIOTimeout = 2 * time.Second
const TerminalDetachTimeout = 4 * time.Second
const MaxTerminalInput = 16 * 1024
const MaxTerminalOutput = 32 * 1024
const TerminalHeader = 9

const (
	TerminalOutput byte = iota
	TerminalInput
	TerminalInputAck
)

var targetPattern = regexp.MustCompile(`^\$[0-9]{1,16}$`)
var epochPattern = regexp.MustCompile(`^e_[a-f0-9]{64}$`)

type TerminalOffer struct {
	Type       string    `json:"type"`
	AgentID    string    `json:"agentId"`
	Generation int64     `json:"generation"`
	Ticket     string    `json:"ticket"`
	Target     string    `json:"sessionId"`
	Epoch      string    `json:"epoch"`
	Mode       string    `json:"mode"`
	Deadline   time.Time `json:"deadline"`
}

func (o TerminalOffer) Valid(id string, generation int64, now time.Time) bool {
	return o.Type == TerminalOpen && o.AgentID == id && o.Generation == generation &&
		ValidSecret(o.Ticket) && targetPattern.MatchString(o.Target) && epochPattern.MatchString(o.Epoch) &&
		(o.Mode == "read_only" || o.Mode == "interactive") &&
		!o.Deadline.IsZero() && !o.Deadline.After(now.Add(TerminalSetupTimeout+5*time.Second))
}

type TerminalEvent struct {
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

func TerminalFrame(direction byte, sequence uint64, payload []byte) []byte {
	frame := make([]byte, TerminalHeader+len(payload))
	frame[0] = direction
	binary.BigEndian.PutUint64(frame[1:TerminalHeader], sequence)
	copy(frame[TerminalHeader:], payload)
	return frame
}

func ParseTerminalFrame(frame []byte) (direction byte, sequence uint64, payload []byte, ok bool) {
	if len(frame) < TerminalHeader {
		return 0, 0, nil, false
	}
	direction, sequence, payload = frame[0], binary.BigEndian.Uint64(frame[1:TerminalHeader]), frame[TerminalHeader:]
	if sequence == 0 {
		return direction, sequence, nil, false
	}
	switch direction {
	case TerminalInput:
		ok = len(payload) > 0 && len(payload) <= MaxTerminalInput
	case TerminalOutput:
		ok = len(payload) > 0 && len(payload) <= MaxTerminalOutput
	case TerminalInputAck:
		ok = len(payload) == 0
	}
	return direction, sequence, payload, ok
}

func ValidTerminalStatus(code string) bool {
	switch code {
	case "detached", "agent_closed", "terminal_target_changed", "terminal_size_policy", "terminal_detach_policy",
		"terminal_attach_failed", "pty_unavailable", "timeout", "command_failed", "tmux_missing",
		"output_limit", "slow_consumer", "input_invalid", "input_forbidden", "input_uncertain":
		return true
	}
	return false
}
