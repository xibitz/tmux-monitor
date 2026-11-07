package tmux

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalid = errors.New("inventory_invalid")
var ErrChanged = errors.New("inventory_changed")

const MaxOutput = 1 << 20

type Capabilities struct {
	Inventory       bool   `json:"inventory"`
	PTYAttach       bool   `json:"ptyAttach"`
	ControlMode     bool   `json:"controlMode"`
	IndependentPane bool   `json:"independentPane"`
	TmuxVersion     string `json:"tmuxVersion"`
	ServerRunning   bool   `json:"serverRunning"`
}
type Inventory struct {
	HostID         string       `json:"hostId"`
	SocketID       string       `json:"socketId"`
	Transport      string       `json:"transport"`
	State          string       `json:"state"`
	StatusReason   string       `json:"statusReason"`
	ObservedAt     string       `json:"observedAt"`
	Stale          bool         `json:"stale"`
	Epoch          string       `json:"epoch"`
	CreateRevision string       `json:"createRevision"`
	Capabilities   Capabilities `json:"capabilities"`
	Sessions       []Session    `json:"sessions"`
}
type Session struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CreatedAt      int64    `json:"createdAt"`
	Revision       string   `json:"revision"`
	RenameRevision string   `json:"renameRevision"`
	Windows        []Window `json:"windows"`
}
type Window struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Index    int    `json:"index"`
	Active   bool   `json:"active"`
	Revision string `json:"revision"`
	Panes    []Pane `json:"panes"`
}
type Pane struct {
	ID       string `json:"id"`
	Active   bool   `json:"active"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Command  string `json:"command"`
	Revision string `json:"revision"`
}

var socketName = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$`)
var nativeID = regexp.MustCompile(`^[$@%][0-9]+$`)
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+[a-z]?$`)

// ShellQuote serializes single argv entries; callers must still use the fixed
// command catalog. No format is sourced from user input or expanded with E:.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func SnapshotCommand(socket string) (string, error) {
	return snapshotCommand(socket, false)
}

// EmptySnapshotCommand rechecks an idle server without commands that require a
// current window/pane. If a session appears meanwhile, parsing rejects the
// incomplete tree instead of claiming an empty inventory.
func EmptySnapshotCommand(socket string) (string, error) { return snapshotCommand(socket, true) }

func snapshotCommand(socket string, empty bool) (string, error) {
	if !socketName.MatchString(socket) {
		return "", ErrInvalid
	}
	// display-message normalizes tabs when no session exists. Printable pipes
	// keep the server fingerprint intact; path pipes are decoded separately.
	fingerprint := "F|#{pid}|#{start_time}|#{q:socket_path}|#{version}"
	args := []string{"tmux", "-L", socket, "display-message", "-p", fingerprint, ";",
		"list-sessions", "-F", "S\t#{session_id}\t#{q:session_name}\t#{session_created}", ";",
		"list-windows", "-a", "-F", "W\t#{session_id}\t#{window_id}\t#{q:window_name}\t#{window_index}\t#{window_active}", ";",
		"list-panes", "-a", "-F", "P\t#{window_id}\t#{pane_id}\t#{pane_active}\t#{pane_width}\t#{pane_height}\t#{q:pane_current_command}", ";",
		"display-message", "-p", fingerprint}
	if empty {
		args = []string{"tmux", "-L", socket, "display-message", "-p", fingerprint, ";", "list-sessions", "-F", "S\t#{session_id}\t#{q:session_name}\t#{session_created}", ";", "display-message", "-p", fingerprint}
	}
	for i := range args {
		args[i] = ShellQuote(args[i])
	}
	return "LC_ALL=C " + strings.Join(args, " "), nil
}

func Empty(host, socket, version string) Inventory {
	return Inventory{HostID: host, SocketID: socket, Transport: "ssh", State: "ONLINE", StatusReason: "no_tmux_server", Sessions: []Session{}, Capabilities: Capabilities{Inventory: true, TmuxVersion: version}}
}

func Parse(host, socket, version string, output []byte) (Inventory, error) {
	inv := Empty(host, socket, version)
	if len(output) == 0 || len(output) > MaxOutput || !utf8.Valid(output) {
		return inv, ErrInvalid
	}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) < 2 || len(lines) > 10000 {
		return inv, ErrInvalid
	}
	first, last := fingerprintFields(lines[0]), fingerprintFields(lines[len(lines)-1])
	if !validFingerprint(first) || !validFingerprint(last) {
		return inv, ErrInvalid
	}
	if lines[0] != lines[len(lines)-1] {
		return inv, ErrChanged
	}
	inv.Epoch = digest("e_", host, socket, lines[0])
	inv.StatusReason = "ok"
	inv.Capabilities.ServerRunning = true
	inv.Capabilities.TmuxVersion = first[4]
	sessions := map[string]int{}
	windows := map[string][]Window{}
	panes := map[string][]Pane{}
	windowIDs := map[string]bool{}
	windowLinks := map[string]bool{}
	paneRecords := map[string]string{}
	for _, line := range lines[1 : len(lines)-1] {
		fields := strings.Split(line, "\t")
		switch fields[0] {
		case "S":
			if len(fields) != 4 || !validID(fields[1], '$') {
				return inv, ErrInvalid
			}
			if _, exists := sessions[fields[1]]; exists {
				return inv, ErrInvalid
			}
			name, err := unquote(fields[2])
			if err != nil {
				return inv, err
			}
			created, err := strconv.ParseInt(fields[3], 10, 64)
			if err != nil || created < 1 {
				return inv, ErrInvalid
			}
			sessions[fields[1]] = len(inv.Sessions)
			inv.Sessions = append(inv.Sessions, Session{ID: fields[1], Name: name, CreatedAt: created, Windows: []Window{}})
		case "W":
			if len(fields) != 6 || !validID(fields[1], '$') || !validID(fields[2], '@') || !flag(fields[5]) {
				return inv, ErrInvalid
			}
			key := fields[1] + ":" + fields[2]
			if windowLinks[key] {
				return inv, ErrInvalid
			}
			windowLinks[key] = true
			name, err := unquote(fields[3])
			if err != nil {
				return inv, err
			}
			index, err := number(fields[4], 0)
			if err != nil {
				return inv, err
			}
			windows[fields[1]] = append(windows[fields[1]], Window{ID: fields[2], Name: name, Index: index, Active: fields[5] == "1", Panes: []Pane{}})
			windowIDs[fields[2]] = true
		case "P":
			if len(fields) != 7 || !validID(fields[1], '@') || !validID(fields[2], '%') || !flag(fields[3]) {
				return inv, ErrInvalid
			}
			if previous, exists := paneRecords[fields[2]]; exists {
				if previous != line {
					return inv, ErrChanged
				}
				continue
			}
			paneRecords[fields[2]] = line
			width, err := number(fields[4], 1)
			if err != nil {
				return inv, err
			}
			height, err := number(fields[5], 1)
			if err != nil {
				return inv, err
			}
			command, err := unquote(fields[6])
			if err != nil {
				return inv, err
			}
			panes[fields[1]] = append(panes[fields[1]], Pane{ID: fields[2], Active: fields[3] == "1", Width: width, Height: height, Command: command, Revision: digest("r_", inv.Epoch, line)})
		default:
			return inv, ErrInvalid
		}
	}
	for sid, ws := range windows {
		index, exists := sessions[sid]
		if !exists {
			return inv, ErrChanged
		}
		for i := range ws {
			ps := panes[ws[i].ID]
			if len(ps) == 0 {
				return inv, ErrChanged
			}
			ws[i].Panes = append([]Pane{}, ps...)
			ws[i].Revision = digest("r_", inv.Epoch, sid, canonical(ws[i]))
		}
		inv.Sessions[index].Windows = ws
	}
	for wid := range panes {
		if !windowIDs[wid] {
			return inv, ErrChanged
		}
	}
	for i := range inv.Sessions {
		if len(inv.Sessions[i].Windows) == 0 {
			return inv, ErrChanged
		}
		s := &inv.Sessions[i]
		s.RenameRevision = digest("rn_", inv.Epoch, s.ID, s.Name, strconv.FormatInt(s.CreatedAt, 10))
		s.Revision = digest("r_", inv.Epoch, canonical(s))
	}
	inv.CreateRevision = createRevision(inv)
	return inv, nil
}

func fingerprintFields(line string) []string {
	parts := strings.SplitN(line, "|", 4)
	if len(parts) != 4 {
		return nil
	}
	last := strings.LastIndexByte(parts[3], '|')
	if last < 0 {
		return nil
	}
	return []string{parts[0], parts[1], parts[2], parts[3][:last], parts[3][last+1:]}
}

func validFingerprint(fields []string) bool {
	if len(fields) != 5 || fields[0] != "F" || len(fields[4]) > 16 || !versionPattern.MatchString(fields[4]) {
		return false
	}
	pid, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || pid == 0 {
		return false
	}
	start, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil || start == 0 {
		return false
	}
	path, err := unquote(fields[3])
	return err == nil && strings.HasPrefix(path, "/")
}
func validID(id string, prefix byte) bool {
	return len(id) > 1 && len(id) < 24 && id[0] == prefix && nativeID.MatchString(id)
}
func flag(s string) bool { return s == "0" || s == "1" }
func number(s string, min int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > 1<<30 {
		return 0, ErrInvalid
	}
	return n, nil
}
func unquote(s string) (string, error) {
	if len(s) > 8192 {
		return "", ErrInvalid
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			if i == len(s) {
				return "", ErrInvalid
			}
		}
		b.WriteByte(s[i])
	}
	value := b.String()
	if !utf8.ValidString(value) {
		return "", ErrInvalid
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", ErrInvalid
		}
	}
	return value, nil
}
func canonical(value any) string { b, _ := json.Marshal(value); return string(b) }
func digest(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s", len(p), p)
	}
	return prefix + hex.EncodeToString(h.Sum(nil))
}
