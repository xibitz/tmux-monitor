package tmux

import (
	"errors"
	"strconv"
)

var ErrLastWindow = errors.New("last_window")
var ErrLastPane = errors.New("last_pane")

type LayoutWindowRequest struct {
	SessionID string `json:"sessionId"`
	Epoch     string `json:"epoch"`
	Revision  string `json:"revision"`
	Confirmed bool   `json:"confirmed"`
}

type KillWindowRequest struct {
	LayoutWindowRequest
	Confirmation string `json:"confirmation"`
}

type PaneLayoutRequest struct {
	LayoutWindowRequest
	WindowID string `json:"windowId"`
}

type SplitPaneRequest struct {
	PaneLayoutRequest
	Direction string `json:"direction"`
}

type KillPaneRequest struct {
	PaneLayoutRequest
	Confirmation string `json:"confirmation"`
}

func ValidPaneID(id string) bool { return validID(id, '%') }

func validLayoutWindow(windowID string, request LayoutWindowRequest) bool {
	return ValidWindowID(windowID) && ValidSessionID(request.SessionID) && epochValue.MatchString(request.Epoch) &&
		treeRevisionValue.MatchString(request.Revision) && request.Confirmed
}

func ValidKillWindow(windowID string, request KillWindowRequest) bool {
	return validLayoutWindow(windowID, request.LayoutWindowRequest) && request.Confirmation == windowID
}

func ValidSplitPane(paneID string, request SplitPaneRequest) bool {
	return ValidPaneID(paneID) && validLayoutWindow(request.WindowID, request.LayoutWindowRequest) &&
		(request.Direction == "side-by-side" || request.Direction == "stacked")
}

func ValidKillPane(paneID string, request KillPaneRequest) bool {
	return ValidPaneID(paneID) && validLayoutWindow(request.WindowID, request.LayoutWindowRequest) &&
		request.Confirmation == paneID
}

func layoutWindowGuard(host, socket, windowID string, request LayoutWindowRequest, output []byte) (*Session, *Window, string, error) {
	inv, fields, err := lifecycleSnapshot(host, socket, output)
	if err != nil {
		return nil, nil, "", err
	}
	if inv.Epoch != request.Epoch {
		return nil, nil, "", ErrChanged
	}
	session, window, links := findWindowLink(inv, request.SessionID, windowID)
	if window == nil {
		return nil, nil, "", ErrNotFound
	}
	if window.Revision != request.Revision {
		return nil, nil, "", ErrChanged
	}
	if links != 1 {
		return nil, nil, "", ErrWindowLinked
	}
	state, err := windowStateGuard(*window)
	if err != nil {
		return nil, nil, "", err
	}
	return session, window, guardAll(serverGuard(fields), identityGuard(*session), state,
		guardField("session_grouped", "0"), guardField("window_linked", "0"),
		guardField("window_panes", strconv.Itoa(len(window.Panes)))), nil
}

func KillWindowCommand(host, socket, windowID string, request KillWindowRequest, output []byte) (string, error) {
	if !ValidKillWindow(windowID, request) {
		return "", ErrInvalid
	}
	session, _, guard, err := layoutWindowGuard(host, socket, windowID, request.LayoutWindowRequest, output)
	if err != nil {
		return "", err
	}
	if len(session.Windows) < 2 {
		return "", ErrLastWindow
	}
	target := session.ID + ":" + windowID
	guard = guardAll(guard, guardField("session_windows", strconv.Itoa(len(session.Windows))))
	branch := "kill-window -t '" + target + "' ; display-message -p MTM_WINDOW_KILLED"
	return serializeGuardedCommand(socket, target, guard, branch)
}

func paneLayoutGuard(host, socket, paneID string, request PaneLayoutRequest, output []byte) (string, string, int, error) {
	session, window, guard, err := layoutWindowGuard(host, socket, request.WindowID, request.LayoutWindowRequest, output)
	if err != nil {
		return "", "", 0, err
	}
	for _, pane := range window.Panes {
		if pane.ID == paneID {
			target := session.ID + ":" + window.ID + "." + paneID
			guard = guardAll(guard, guardField("pane_id", paneID), guardField("window_zoomed_flag", "0"))
			return target, guard, len(window.Panes), nil
		}
	}
	return "", "", 0, ErrNotFound
}

func SplitPaneCommand(host, socket, paneID string, request SplitPaneRequest, output []byte) (string, error) {
	if !ValidSplitPane(paneID, request) {
		return "", ErrInvalid
	}
	target, guard, count, err := paneLayoutGuard(host, socket, paneID, request.PaneLayoutRequest, output)
	if err != nil {
		return "", err
	}
	if count >= MaxLifecycleItems {
		return "", ErrLifecycleLimit
	}
	direction := "-v"
	if request.Direction == "side-by-side" {
		direction = "-h"
	}
	branch := "split-window -d " + direction + " -t '" + target + "' -P -F 'MTM_PANE_CREATED|#{pane_id}'"
	return serializeGuardedCommand(socket, target, guard, branch)
}

func KillPaneCommand(host, socket, paneID string, request KillPaneRequest, output []byte) (string, error) {
	if !ValidKillPane(paneID, request) {
		return "", ErrInvalid
	}
	target, guard, count, err := paneLayoutGuard(host, socket, paneID, request.PaneLayoutRequest, output)
	if err != nil {
		return "", err
	}
	if count < 2 {
		return "", ErrLastPane
	}
	branch := "kill-pane -t '" + target + "' ; display-message -p MTM_PANE_KILLED"
	return serializeGuardedCommand(socket, target, guard, branch)
}

func CreatedPaneID(output []byte) (string, error) {
	return createdID(output, "MTM_PANE_CREATED|", '%')
}
