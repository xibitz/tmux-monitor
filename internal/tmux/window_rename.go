package tmux

import (
	"errors"
	"strings"
)

var ErrWindowLinked = errors.New("window_linked")

type RenameWindowRequest struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Epoch     string `json:"epoch"`
	Revision  string `json:"revision"`
	Confirmed bool   `json:"confirmed"`
}

func ValidWindowID(id string) bool { return validID(id, '@') }

func ValidRenameWindow(windowID string, request RenameWindowRequest) bool {
	return ValidWindowID(windowID) && ValidSessionID(request.SessionID) && renameName.MatchString(request.Name) &&
		epochValue.MatchString(request.Epoch) && treeRevisionValue.MatchString(request.Revision) && request.Confirmed
}

func RenameWindowCommand(host, socket, windowID string, request RenameWindowRequest, output []byte) (string, error) {
	if !ValidRenameWindow(windowID, request) {
		return "", ErrInvalid
	}
	inv, fields, err := lifecycleSnapshot(host, socket, output)
	if err != nil {
		return "", err
	}
	if inv.Epoch != request.Epoch {
		return "", ErrChanged
	}
	var session *Session
	var window *Window
	links := 0
	for i := range inv.Sessions {
		current := &inv.Sessions[i]
		for j := range current.Windows {
			candidate := &current.Windows[j]
			if candidate.ID == windowID {
				links++
				if current.ID == request.SessionID {
					session, window = current, candidate
				}
			}
		}
	}
	if window == nil {
		return "", ErrNotFound
	}
	if window.Revision != request.Revision {
		return "", ErrChanged
	}
	if links != 1 {
		return "", ErrWindowLinked
	}
	if len(session.Windows) > MaxLifecycleItems {
		return "", ErrLifecycleLimit
	}
	for _, sibling := range session.Windows {
		if sibling.ID != windowID && sibling.Name == request.Name {
			return "", ErrLifecycleDuplicate
		}
	}
	state, err := windowStateGuard(*window)
	if err != nil {
		return "", err
	}
	// Every sibling emits a digit. A failed or empty loop cannot satisfy the guard.
	duplicate := guardAll("#{!=:#{window_id},"+windowID+"}", guardField("window_name", request.Name))
	unique := guardEqual("#{W:#{?"+duplicate+",1,0}}", strings.Repeat("0", len(session.Windows)))
	guard := guardAll(serverGuard(fields), identityGuard(*session), state, guardField("window_linked", "0"), unique)
	target := session.ID + ":" + windowID
	branch := "rename-window -t '" + target + "' '" + request.Name + "' ; display-message -p MTM_WINDOW_RENAMED"
	return serializeGuardedCommand(socket, target, guard, branch)
}
