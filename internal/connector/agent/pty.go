package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/xibitz/tmux-monitor/internal/tmux"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type localPTY struct {
	master     *os.File
	cancel     context.CancelFunc
	done       chan struct{}
	tty        string
	policy     tmux.TerminalPolicy
	host       string
	socket     string
	target     string
	epoch      string
	policyArgs []string
	clientArgs []string
}

func openPTY(ctx, setup context.Context, host, socket, target, epoch string, interactive bool) (*localPTY, string) {
	policyArgs, err := tmux.TerminalPolicyArgs(socket, target)
	if err != nil {
		return nil, "terminal_target_changed"
	}
	policyArgs = append(policyArgs, ";", "show-options", "-sv", "exit-unattached",
		";", "show-options", "-Av", "-t", target+":", "destroy-unattached")
	clientArgs, err := tmux.ClientTargetsArgs(socket)
	if err != nil {
		return nil, "terminal_target_changed"
	}
	attach, err := tmux.LocalAttachArgs(socket, target, interactive)
	if err != nil {
		return nil, "terminal_target_changed"
	}
	t := &localPTY{host: host, socket: socket, target: target, epoch: epoch, policyArgs: policyArgs, clientArgs: clientArgs}
	policy, status := t.readPolicy(setup)
	if status != "ok" {
		return nil, status
	}
	t.policy = policy
	master, slave, err := pty.Open()
	if err != nil {
		return nil, "pty_unavailable"
	}
	defer master.Close()
	defer slave.Close()
	t.tty = slave.Name()
	if !tmux.ValidTTY(t.tty) {
		return nil, "pty_unavailable"
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: uint16(policy.Cols), Rows: uint16(policy.Rows)}); err != nil {
		return nil, "pty_unavailable"
	}
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		return nil, "pty_unavailable"
	}
	// Register a nonblocking master with Go's poller so Close and write
	// deadlines interrupt blocked PTY I/O on both supported platforms.
	fd, err := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, "pty_unavailable"
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, "pty_unavailable"
	}
	t.master = os.NewFile(uintptr(fd), master.Name())
	if t.master == nil {
		_ = unix.Close(fd)
		return nil, "pty_unavailable"
	}
	if err := t.master.SetDeadline(time.Time{}); err != nil {
		t.master.Close()
		return nil, "pty_unavailable"
	}
	lifetime, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	cmd := exec.CommandContext(lifetime, attach[0], attach[1:]...)
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		return key == "TMUX" || key == "TMUX_PANE"
	}), "LC_ALL=C", "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if setup.Err() != nil {
		cancel()
		t.master.Close()
		return nil, "timeout"
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.master.Close()
		return nil, "terminal_attach_failed"
	}
	_ = slave.Close()
	_ = master.Close()
	t.done = make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(t.done)
	}()
	for {
		if status = t.checkClient(setup); status == "ok" {
			break
		}
		if status != "terminal_target_changed" {
			t.close()
			return nil, status
		}
		select {
		case <-t.done:
			t.close()
			return nil, "terminal_attach_failed"
		case <-setup.Done():
			t.close()
			return nil, "timeout"
		case <-time.After(20 * time.Millisecond):
		}
	}
	if status = t.check(setup); status != "ok" {
		t.close()
		return nil, status
	}
	return t, "ok"
}

func (t *localPTY) readPolicy(ctx context.Context) (tmux.TerminalPolicy, string) {
	out, _, status := localTmux(ctx, tmux.MaxOutput, append([]string{"-N"}, t.policyArgs[1:]...)...)
	if status != "ok" {
		return tmux.TerminalPolicy{}, status
	}
	out, safe := bytes.CutSuffix(out, []byte("off\noff\n"))
	if !safe {
		return tmux.TerminalPolicy{}, "terminal_detach_policy"
	}
	policy, err := tmux.ParseTerminalPolicy(t.host, t.socket, t.target, t.epoch, out)
	if errors.Is(err, tmux.ErrChanged) {
		return policy, "terminal_target_changed"
	}
	if err != nil {
		return policy, "terminal_size_policy"
	}
	return policy, "ok"
}

func (t *localPTY) checkClient(ctx context.Context) string {
	out, _, status := localTmux(ctx, tmux.MaxOutput, append([]string{"-N"}, t.clientArgs[1:]...)...)
	if status != "ok" {
		return status
	}
	if tmux.CheckClientTarget(out, t.tty, t.target) != nil {
		return "terminal_target_changed"
	}
	return "ok"
}

func (t *localPTY) check(ctx context.Context) string {
	policy, status := t.readPolicy(ctx)
	if status != "ok" {
		return status
	}
	if policy.Signature != t.policy.Signature {
		return "terminal_target_changed"
	}
	return t.checkClient(ctx)
}

func (t *localPTY) close() {
	t.cancel()
	_ = t.master.Close()
	<-t.done
}
