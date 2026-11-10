package tmux

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxPaneSnapshotRaw = 256 * 1024
const MaxPaneSnapshotText = 64 * 1024
const paneSnapshotMarker = "MTM_PANE_SNAPSHOT\n"

type PaneSnapshotRequest struct {
	Epoch    string `json:"epoch"`
	Revision string `json:"revision"`
}

type PaneSnapshot struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

func ValidPaneSnapshot(pane string, request PaneSnapshotRequest) bool {
	return validID(pane, '%') && epochValue.MatchString(request.Epoch) && treeRevisionValue.MatchString(request.Revision)
}

func PaneSnapshotCommand(host, socket, paneID string, request PaneSnapshotRequest, output []byte) (string, error) {
	if !ValidPaneSnapshot(paneID, request) {
		return "", ErrInvalid
	}
	inv, fields, err := lifecycleSnapshot(host, socket, output)
	if err != nil {
		return "", err
	}
	if inv.Epoch != request.Epoch {
		return "", ErrChanged
	}
	for _, session := range inv.Sessions {
		for _, window := range session.Windows {
			for _, pane := range window.Panes {
				if pane.ID != paneID {
					continue
				}
				if pane.Revision != request.Revision {
					return "", ErrChanged
				}
				guard := guardAll(serverGuard(fields), guardField("window_id", window.ID), guardField("pane_id", paneID),
					guardField("pane_active", boolText(pane.Active)), guardField("pane_width", strconv.Itoa(pane.Width)),
					guardField("pane_height", strconv.Itoa(pane.Height)), guardField("pane_current_command", pane.Command))
				// No -e or buffer target: emit plain stdout, without creating a paste buffer.
				branch := "display-message -p MTM_PANE_SNAPSHOT ; capture-pane -p -S -100 -t '" + paneID + "'"
				args := []string{"tmux", "-N", "-L", socket, "if-shell", "-F", "-t", paneID, guard, branch, "display-message -p MTM_CONFLICT"}
				for i := range args {
					args[i] = ShellQuote(args[i])
				}
				command := "LC_ALL=C " + strings.Join(args, " ")
				if len(command) > MaxLifecycleCommand {
					return "", ErrLifecycleLimit
				}
				return command, nil
			}
		}
	}
	return "", ErrNotFound
}

func ParsePaneSnapshot(output []byte) (PaneSnapshot, error) {
	if bytes.Equal(output, []byte("MTM_CONFLICT\n")) {
		return PaneSnapshot{}, ErrChanged
	}
	payload, ok := bytes.CutPrefix(output, []byte(paneSnapshotMarker))
	if !ok || len(output) > MaxPaneSnapshotRaw {
		return PaneSnapshot{}, ErrInvalid
	}
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		// Do not retain terminal control bytes or invisible direction overrides.
		if unicode.IsControl(r) || r == '\u061c' || r == '\u200e' || r == '\u200f' ||
			r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069' {
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(payload), "\uFFFD"))
	truncated := len(clean) > MaxPaneSnapshotText
	if truncated {
		start := len(clean) - MaxPaneSnapshotText
		for start < len(clean) && !utf8.RuneStart(clean[start]) {
			start++
		}
		clean = clean[start:]
	}
	return PaneSnapshot{Text: clean, Truncated: truncated}, nil
}
