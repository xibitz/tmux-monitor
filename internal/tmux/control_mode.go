package tmux

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
)

var ErrControlFrame = errors.New("invalid tmux control frame")

const maxControlLine = 128 << 10

type ControlEvent struct {
	Kind string
	Pane string
	Data []byte
}

// ReadControl reads tmux -C (without the -CC terminal envelope). Output in
// a command block is opaque, even if it starts with a notification marker.
func ReadControl(r io.Reader, emit func(ControlEvent)) error {
	reader := bufio.NewReaderSize(r, maxControlLine)
	var guard string
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			if err == io.EOF && len(line) == 0 && guard == "" {
				return nil
			}
			return ErrControlFrame
		}
		line = bytes.TrimSuffix(line, []byte("\n"))
		name, value, _ := strings.Cut(string(line), " ")
		if guard != "" {
			if (name == "%end" || name == "%error") && value == guard {
				guard = ""
				if name == "%error" {
					emit(ControlEvent{Kind: "command.error"})
				}
			}
			continue
		}
		switch name {
		case "%begin":
			fields := strings.Split(value, " ")
			if len(fields) != 3 {
				return ErrControlFrame
			}
			for _, field := range fields {
				if _, err := strconv.ParseUint(field, 10, 64); err != nil {
					return ErrControlFrame
				}
			}
			guard = value
		case "%end", "%error":
			return ErrControlFrame
		case "%output", "%extended-output":
			pane, payload, found := strings.Cut(value, " ")
			if !found || !ValidPaneID(pane) {
				return ErrControlFrame
			}
			if name == "%extended-output" {
				metadata, data, found := strings.Cut(payload, " : ")
				age, _, _ := strings.Cut(metadata, " ")
				if _, err := strconv.ParseUint(age, 10, 64); !found || err != nil {
					return ErrControlFrame
				}
				payload = data
			}
			data, err := unescapeControl(payload)
			if err != nil {
				return err
			}
			emit(ControlEvent{Kind: "pane.output", Pane: pane, Data: data})
		case "%sessions-changed", "%session-renamed", "%session-window-changed", "%window-add", "%window-close", "%window-renamed", "%unlinked-window-add", "%unlinked-window-close", "%unlinked-window-renamed", "%window-pane-changed", "%layout-change", "%pane-mode-changed":
			emit(ControlEvent{Kind: "inventory.changed"})
		case "%exit":
			emit(ControlEvent{Kind: "client.state"})
			return nil
		case "%session-changed", "%client-session-changed", "%client-detached", "%pause", "%continue":
			emit(ControlEvent{Kind: "client.state"})
		default:
			if !strings.HasPrefix(name, "%") {
				return ErrControlFrame
			}
			// New tmux notifications are forward compatible. Periodic inventory
			// reconciliation covers changes not understood by this reader.
		}
	}
}

func unescapeControl(value string) ([]byte, error) {
	out := make([]byte, 0, len(value))
	for len(value) != 0 {
		if value[0] != '\\' {
			out = append(out, value[0])
			value = value[1:]
			continue
		}
		if len(value) < 4 || value[1] < '0' || value[1] > '3' ||
			value[2] < '0' || value[2] > '7' || value[3] < '0' || value[3] > '7' {
			return nil, ErrControlFrame
		}
		out = append(out, (value[1]-'0')<<6|(value[2]-'0')<<3|(value[3]-'0'))
		value = value[4:]
	}
	return out, nil
}

// ControlArgs never creates a server, resizes a client, or sends user input.
// -E keeps the session environment unchanged when attaching.
func ControlArgs(socket string) ([]string, error) {
	if !socketName.MatchString(socket) {
		return nil, ErrInvalid
	}
	const attach = "attach-session -E -f read-only,ignore-size,no-output"
	return []string{"tmux", "-N", "-L", socket, "-C", "if-shell", "-F", safeDetachFormat, attach}, nil
}
