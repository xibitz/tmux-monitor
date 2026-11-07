package tmux

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Source-only additions: not yet qualified by tests. HTTP gates them separately
// from the previously verified rename path.
const MaxLifecycleItems = 64
const MaxLifecycleCommand = 64 * 1024

var ErrLifecycleLimit = errors.New("mutation_limit")
var ErrLifecycleDuplicate = errors.New("duplicate_name")
var ErrLifecycleUnsupported = errors.New("tmux_unsupported")
var createRevisionValue = regexp.MustCompile(`^cr_[a-f0-9]{64}$`)
var treeRevisionValue = regexp.MustCompile(`^r_[a-f0-9]{64}$`)

func ValidSessionID(id string) bool { return validID(id, '$') }

type CreateSessionRequest struct {
	Name      string `json:"name"`
	Epoch     string `json:"epoch"`
	Revision  string `json:"revision"`
	Confirmed bool   `json:"confirmed"`
}
type KillSessionRequest struct {
	Epoch        string `json:"epoch"`
	Revision     string `json:"revision"`
	Confirmation string `json:"confirmation"`
	Confirmed    bool   `json:"confirmed"`
}

func ValidCreateSession(r CreateSessionRequest) bool {
	return renameName.MatchString(r.Name) && epochValue.MatchString(r.Epoch) && createRevisionValue.MatchString(r.Revision) && r.Confirmed
}
func ValidKillSession(session string, r KillSessionRequest) bool {
	return validID(session, '$') && epochValue.MatchString(r.Epoch) && treeRevisionValue.MatchString(r.Revision) && r.Confirmation == session && r.Confirmed
}

type sessionIdentity struct {
	ID, Name  string
	CreatedAt int64
}

func createRevision(inv Inventory) string {
	identities := make([]sessionIdentity, 0, len(inv.Sessions))
	for _, s := range inv.Sessions {
		identities = append(identities, sessionIdentity{s.ID, s.Name, s.CreatedAt})
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i].ID < identities[j].ID })
	return digest("cr_", inv.Epoch, canonical(identities))
}

func guardEqual(format, expected string) string {
	return "#{==:" + format + "," + formatLiteral(expected) + "}"
}
func guardField(key, expected string) string { return guardEqual("#{"+key+"}", expected) }
func guardAll(conditions ...string) string {
	if len(conditions) == 0 {
		return "1"
	}
	if len(conditions) == 1 {
		return conditions[0]
	}
	middle := len(conditions) / 2
	return "#{&&:" + guardAll(conditions[:middle]...) + "," + guardAll(conditions[middle:]...) + "}"
}
func guardMatch(loop, identityKey, id, condition string) string {
	// Matching IDs emit one boolean; every other iteration emits an empty string.
	// Avoid ambiguous serialization of host-controlled names into delimiters.
	return guardEqual("#{"+loop+":#{?"+guardField(identityKey, id)+","+condition+",}}", "1")
}
func guardCount(loop string, n int) string {
	return guardEqual("#{"+loop+":x}", strings.Repeat("x", n))
}
func boolText(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func lifecycleSnapshot(host, socket string, output []byte) (Inventory, []string, error) {
	if !socketName.MatchString(socket) {
		return Inventory{}, nil, ErrInvalid
	}
	inv, err := Parse(host, socket, "3.8", output)
	if err != nil {
		return inv, nil, err
	}
	fields := fingerprintFields(strings.SplitN(string(output), "\n", 2)[0])
	if fields[4] != "3.8" {
		return inv, nil, ErrLifecycleUnsupported
	}
	return inv, fields, nil
}
func serverGuard(fields []string) string {
	return guardAll(guardField("pid", fields[1]), guardField("start_time", fields[2]), guardField("q:socket_path", fields[3]), guardField("version", fields[4]))
}
func identityGuard(s Session) string {
	return guardAll(guardField("session_id", s.ID), guardField("session_created", strconv.FormatInt(s.CreatedAt, 10)), guardField("session_name", s.Name))
}
func serializeLifecycle(socket, target, condition, branch string) (string, error) {
	args := []string{"tmux", "-N", "-L", socket, "if-shell", "-F"}
	if target != "" {
		args = append(args, "-t", target+":")
	}
	args = append(args, condition, branch, "display-message -p MTM_CONFLICT")
	for i := range args {
		args[i] = ShellQuote(args[i])
	}
	command := "LC_ALL=C " + strings.Join(args, " ")
	if len(command) > MaxLifecycleCommand {
		return "", ErrLifecycleLimit
	}
	return command, nil
}

func CreateSessionCommand(host, socket string, r CreateSessionRequest, output []byte) (string, error) {
	if !ValidCreateSession(r) {
		return "", ErrInvalid
	}
	inv, fields, err := lifecycleSnapshot(host, socket, output)
	if err != nil {
		return "", err
	}
	if inv.Epoch != r.Epoch || inv.CreateRevision != r.Revision {
		return "", ErrChanged
	}
	if len(inv.Sessions) >= MaxLifecycleItems {
		return "", ErrLifecycleLimit
	}
	checks := []string{serverGuard(fields), guardCount("S", len(inv.Sessions))}
	size := 0
	for _, session := range inv.Sessions {
		if session.Name == r.Name {
			return "", ErrLifecycleDuplicate
		}
		check := guardMatch("S", "session_id", session.ID, identityGuard(session))
		size += len(check)
		if size > MaxLifecycleCommand {
			return "", ErrLifecycleLimit
		}
		checks = append(checks, check)
	}
	// The global -N prevents server bootstrap. There is no waiting command between
	// guard and new-session; geometry affects only the explicitly new session.
	branch := "new-session -d -s '" + r.Name + "' -x 120 -y 40 -P -F 'MTM_CREATED|#{session_id}'"
	return serializeLifecycle(socket, "", guardAll(checks...), branch)
}

func KillSessionCommand(host, socket, session string, r KillSessionRequest, output []byte) (string, error) {
	if !ValidKillSession(session, r) {
		return "", ErrInvalid
	}
	inv, fields, err := lifecycleSnapshot(host, socket, output)
	if err != nil {
		return "", err
	}
	if inv.Epoch != r.Epoch {
		return "", ErrChanged
	}
	var selected *Session
	for i := range inv.Sessions {
		if inv.Sessions[i].ID == session {
			selected = &inv.Sessions[i]
			break
		}
	}
	if selected == nil {
		return "", ErrNotFound
	}
	if selected.Revision != r.Revision {
		return "", ErrChanged
	}
	if len(selected.Windows) > MaxLifecycleItems {
		return "", ErrLifecycleLimit
	}
	checks := []string{serverGuard(fields), identityGuard(*selected), guardCount("W", len(selected.Windows))}
	size := 0
	for _, window := range selected.Windows {
		if len(window.Panes) > MaxLifecycleItems {
			return "", ErrLifecycleLimit
		}
		windowChecks := []string{guardField("window_id", window.ID), guardField("window_name", window.Name), guardField("window_index", strconv.Itoa(window.Index)), guardField("window_active", boolText(window.Active)), guardCount("P", len(window.Panes))}
		windowSize := 0
		for _, pane := range window.Panes {
			p := guardAll(guardField("pane_id", pane.ID), guardField("pane_active", boolText(pane.Active)), guardField("pane_width", strconv.Itoa(pane.Width)), guardField("pane_height", strconv.Itoa(pane.Height)), guardField("pane_current_command", pane.Command))
			check := guardMatch("P", "pane_id", pane.ID, p)
			windowSize += len(check)
			if windowSize > MaxLifecycleCommand {
				return "", ErrLifecycleLimit
			}
			windowChecks = append(windowChecks, check)
		}
		check := guardMatch("W", "window_id", window.ID, guardAll(windowChecks...))
		size += len(check)
		if size > MaxLifecycleCommand {
			return "", ErrLifecycleLimit
		}
		checks = append(checks, check)
	}
	branch := "kill-session -t '" + session + "' ; display-message -p MTM_KILLED"
	return serializeLifecycle(socket, session, guardAll(checks...), branch)
}

func CreatedSessionID(output []byte) (string, error) {
	const prefix = "MTM_CREATED|"
	value := string(output)
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, "\n") {
		return "", ErrInvalid
	}
	id := strings.TrimSuffix(strings.TrimPrefix(value, prefix), "\n")
	if !validID(id, '$') {
		return "", ErrInvalid
	}
	return id, nil
}
