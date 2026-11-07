package tmux

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var ErrNotFound = errors.New("not_found")
var renameName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`)
var epochValue = regexp.MustCompile(`^e_[a-f0-9]{64}$`)
var renameRevision = regexp.MustCompile(`^rn_[a-f0-9]{64}$`)

type RenameRequest struct {
	Name      string `json:"name"`
	Epoch     string `json:"epoch"`
	Revision  string `json:"revision"`
	Confirmed bool   `json:"confirmed"`
}

func ValidRename(session string, r RenameRequest) bool {
	return validID(session, '$') && renameName.MatchString(r.Name) && epochValue.MatchString(r.Epoch) && renameRevision.MatchString(r.Revision) && r.Confirmed
}

// Format literals are escaped independently from shell/tmux argv quoting.
// Never use E: or T: to re-expand a name or path received from the host.
func formatLiteral(s string) string {
	return strings.NewReplacer("#", "##", ",", "#,", "}", "#}").Replace(s)
}

func RenameCommand(host, socket, session string, request RenameRequest, output []byte) (string, error) {
	if !socketName.MatchString(socket) || !ValidRename(session, request) {
		return "", ErrInvalid
	}
	inv, err := Parse(host, socket, "3.8", output)
	if err != nil {
		return "", err
	}
	if inv.Epoch != request.Epoch {
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
	if selected.RenameRevision != request.Revision {
		return "", ErrChanged
	}
	fields := fingerprintFields(strings.SplitN(string(output), "\n", 2)[0])
	if fields[4] != "3.8" {
		return "", errors.New("tmux_unsupported")
	}
	tests := [][2]string{{"pid", fields[1]}, {"start_time", fields[2]}, {"q:socket_path", fields[3]}, {"version", fields[4]}, {"session_id", session}, {"session_created", strconv.FormatInt(selected.CreatedAt, 10)}, {"session_name", selected.Name}}
	condition := "1"
	for _, test := range tests {
		condition = "#{&&:" + condition + ",#{==:#{" + test[0] + "}," + formatLiteral(test[1]) + "}}"
	}
	// No waiting command occurs between guard and rename. Native ID only; never
	// resolve by old/new name, and never change any viewer or layout setting.
	branch := "rename-session -t '" + session + "' '" + request.Name + "' ; display-message -p MTM_RENAMED"
	args := []string{"tmux", "-L", socket, "if-shell", "-F", "-t", session + ":", condition, branch, "display-message -p MTM_CONFLICT"}
	for i := range args {
		args[i] = ShellQuote(args[i])
	}
	return "LC_ALL=C " + strings.Join(args, " "), nil
}
