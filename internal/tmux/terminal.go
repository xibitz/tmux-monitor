package tmux

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var ttyPath = regexp.MustCompile(`^/dev/(pts/[0-9]{1,10}|tty[a-zA-Z0-9]{1,32})$`)

func ClientTargetsCommand(socket string) (string, error) {
	if !socketName.MatchString(socket) {
		return "", ErrInvalid
	}
	args := []string{"tmux", "-L", socket, "list-clients", "-F", "C|#{client_tty}|#{session_id}"}
	for i := range args {
		args[i] = ShellQuote(args[i])
	}
	return "LC_ALL=C " + strings.Join(args, " "), nil
}

func ValidTTY(value string) bool { return ttyPath.MatchString(value) }

func CheckClientTarget(output []byte, tty, session string) error {
	if !ValidTTY(tty) || !validID(session, '$') || len(output) > MaxOutput {
		return ErrInvalid
	}
	found := false
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != 3 || fields[0] != "C" || !validID(fields[2], '$') {
			return ErrInvalid
		}
		if fields[1] == tty {
			if found || fields[2] != session {
				return ErrChanged
			}
			found = true
		}
	}
	if !found {
		return ErrChanged
	}
	return nil
}

// TerminalPolicy is freshly read from tmux, never from the inventory cache.
type TerminalPolicy struct {
	Cols      int
	Rows      int
	Signature string
}

// Older tmux releases have a server crash when spawning windows with the
// global manual sizing policy (upstream #4849). Do not recommend that policy
// or attach this experimental viewer on those releases.
func TerminalVersionSupported(version string) bool {
	if !versionPattern.MatchString(version) {
		return false
	}
	var major, minor int
	_, err := fmt.Sscanf(version, "%d.%d", &major, &minor)
	return err == nil && (major > 3 || major == 3 && minor >= 7)
}

func TerminalPolicyCommand(socket, session string) (string, error) {
	if !socketName.MatchString(socket) || !validID(session, '$') {
		return "", ErrInvalid
	}
	args := []string{"tmux", "-L", socket, "display-message", "-p", "F|#{pid}|#{start_time}|#{q:socket_path}|#{version}", ";",
		"show-options", "-gwv", "window-size", ";",
		"display-message", "-p", "-t", session + ":", "T|#{session_id}|#{status}|#{window_id}|#{window_width}|#{window_height}", ";",
		"list-windows", "-t", session, "-F", "W|#{window_id}|#{window-size}|#{window_width}|#{window_height}", ";",
		"display-message", "-p", "F|#{pid}|#{start_time}|#{q:socket_path}|#{version}"}
	for i := range args {
		args[i] = ShellQuote(args[i])
	}
	return "LC_ALL=C " + strings.Join(args, " "), nil
}

func AttachCommand(socket, session string) (string, error) {
	return attachCommand(socket, session, false)
}

func InteractiveAttachCommand(socket, session string) (string, error) {
	return attachCommand(socket, session, true)
}

func attachCommand(socket, session string, interactive bool) (string, error) {
	if !socketName.MatchString(socket) || !validID(session, '$') {
		return "", ErrInvalid
	}
	flags := "read-only,ignore-size"
	if interactive {
		flags = "ignore-size"
	}
	args := []string{"tmux", "-L", socket, "attach-session", "-f", flags, "-t", session}
	for i := range args {
		args[i] = ShellQuote(args[i])
	}
	// Capture tty off the PTY, frame it with NUL and wait for a fixed ack. tmux
	// may flush terminal output on startup, so it must not start before the
	// connector has consumed the complete metadata frame. No user input is used.
	return `_tmux_monitor_tty=$(tty) && printf '%s\000' "$_tmux_monitor_tty" && IFS= read -r _tmux_monitor_ready && [ "$_tmux_monitor_ready" = 'ready' ] && exec env LC_ALL=C ` + strings.Join(args, " "), nil
}

func ParseTerminalPolicy(host, socket, session, epoch string, output []byte) (TerminalPolicy, error) {
	var policy TerminalPolicy
	if len(output) > MaxOutput {
		return policy, ErrInvalid
	}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) < 5 || len(lines) > 1000 {
		return policy, ErrInvalid
	}
	first := fingerprintFields(lines[0])
	if !validFingerprint(first) || lines[0] != lines[len(lines)-1] || digest("e_", host, socket, lines[0]) != epoch {
		return policy, ErrChanged
	}
	if !TerminalVersionSupported(first[4]) {
		return policy, ErrInvalid
	}
	if lines[1] != "manual" {
		return policy, ErrInvalid
	}
	target := strings.Split(lines[2], "|")
	if len(target) != 6 || target[0] != "T" || target[1] != session || !validID(target[3], '@') {
		return policy, ErrInvalid
	}
	status := 0
	switch target[2] {
	case "on":
		status = 1
	case "off":
	default:
		n, err := strconv.Atoi(target[2])
		if err != nil || n < 0 || n > 5 {
			return policy, ErrInvalid
		}
		status = n
	}
	cols, err := strconv.Atoi(target[4])
	if err != nil || cols < 2 || cols > 500 {
		return policy, ErrInvalid
	}
	height, err := strconv.Atoi(target[5])
	if err != nil || height < 2 || height > 200 {
		return policy, ErrInvalid
	}
	seen, active := map[string]bool{}, false
	for _, line := range lines[3 : len(lines)-1] {
		w := strings.Split(line, "|")
		if len(w) != 5 || w[0] != "W" || !validID(w[1], '@') || w[2] != "manual" || seen[w[1]] {
			return policy, ErrInvalid
		}
		seen[w[1]] = true
		for _, size := range w[3:] {
			n, e := strconv.Atoi(size)
			if e != nil || n < 2 || n > 500 {
				return policy, ErrInvalid
			}
		}
		if w[1] == target[3] {
			active = w[3] == target[4] && w[4] == target[5]
		}
	}
	if !active {
		return policy, ErrInvalid
	}
	return TerminalPolicy{Cols: cols, Rows: height + status, Signature: digest("p_", lines...)}, nil
}
