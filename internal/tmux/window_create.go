package tmux

import "strconv"

type CreateWindowRequest struct {
	Name      string `json:"name"`
	Epoch     string `json:"epoch"`
	Revision  string `json:"revision"`
	Confirmed bool   `json:"confirmed"`
}

func ValidCreateWindow(sessionID string, request CreateWindowRequest) bool {
	return ValidSessionID(sessionID) && renameName.MatchString(request.Name) &&
		epochValue.MatchString(request.Epoch) && treeRevisionValue.MatchString(request.Revision) && request.Confirmed
}

func CreateWindowCommand(host, socket, sessionID string, request CreateWindowRequest, output []byte) (string, error) {
	if !ValidCreateWindow(sessionID, request) {
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
		if session.ID != sessionID {
			continue
		}
		if session.Revision != request.Revision {
			return "", ErrChanged
		}
		if len(session.Windows) >= MaxLifecycleItems {
			return "", ErrLifecycleLimit
		}
		checks := []string{serverGuard(fields), identityGuard(session),
			guardField("session_grouped", "0"), guardCount("W", len(session.Windows))}
		for _, window := range session.Windows {
			if window.Name == request.Name {
				return "", ErrLifecycleDuplicate
			}
			state := guardAll(guardField("window_name", window.Name), guardField("window_index", strconv.Itoa(window.Index)))
			checks = append(checks, guardMatch("W", "window_id", window.ID, state))
		}
		// Let tmux allocate a free index. Detached creation leaves the current window selected.
		branch := "new-window -d -t '" + sessionID + ":' -n '" + request.Name + "' -P -F 'MTM_WINDOW_CREATED|#{window_id}'"
		return serializeLifecycle(socket, sessionID, guardAll(checks...), branch)
	}
	return "", ErrNotFound
}

func CreatedWindowID(output []byte) (string, error) {
	return createdID(output, "MTM_WINDOW_CREATED|", '@')
}
