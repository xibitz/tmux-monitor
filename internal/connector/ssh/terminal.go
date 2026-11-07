package sshconnector

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/xibitz/tmux-monitor/internal/tmux"
	"golang.org/x/crypto/ssh"
)

// Read-only terminals never expose WriteInput; resizing is not supported.
type Terminal interface {
	io.Reader
	io.Closer
	Size() (int, int)
	Check(context.Context) error
}

type InputTerminal interface {
	Terminal
	WriteInput(context.Context, []byte) error
}

type inputTerminal struct{ *terminal }

func (t *inputTerminal) WriteInput(ctx context.Context, data []byte) error {
	if len(data) == 0 || len(data) > 16384 {
		return fail("input_limit")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return fail("timeout")
	}
	stop := context.AfterFunc(ctx, func() { _ = t.client.Close() })
	defer stop()
	n, err := t.stdin.Write(data)
	if ctx.Err() != nil {
		return fail("timeout")
	}
	if err != nil || n != len(data) {
		_ = t.Close()
		return fail("input_uncertain")
	}
	return nil
}

type terminal struct {
	client                 *ssh.Client
	reader                 io.Reader
	stdin                  io.WriteCloser
	policy                 tmux.TerminalPolicy
	host                   Host
	target, epoch, command string
	clientCommand, tty     string
	once                   sync.Once
	stop                   func() bool
}

func (m *Manager) OpenTerminal(ctx context.Context, h Host, record Record, target, epoch string) (Terminal, error) {
	return m.openTerminal(ctx, h, record, target, epoch, false)
}

func (m *Manager) OpenInteractiveTerminal(ctx context.Context, h Host, record Record, target, epoch string) (Terminal, error) {
	t, err := m.openTerminal(ctx, h, record, target, epoch, true)
	if err != nil {
		return nil, err
	}
	return &inputTerminal{t.(*terminal)}, nil
}

func (m *Manager) openTerminal(ctx context.Context, h Host, record Record, target, epoch string, interactive bool) (Terminal, error) {
	command, err := tmux.TerminalPolicyCommand(h.Socket, target)
	if err != nil {
		return nil, fail("terminal_target_invalid")
	}
	attach, err := tmux.AttachCommand(h.Socket, target)
	if interactive {
		attach, err = tmux.InteractiveAttachCommand(h.Socket, target)
	}
	if err != nil {
		return nil, fail("terminal_target_invalid")
	}
	setup, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	client, conn, err := m.connect(setup, h, record)
	if err != nil {
		return nil, err
	}
	stopSetup := context.AfterFunc(setup, func() { _ = client.Close() })
	defer stopSetup()
	ok := false
	defer func() {
		if !ok {
			_ = client.Close()
		}
	}()
	t := &terminal{client: client, host: h, target: target, epoch: epoch, command: command}
	policy, err := t.readPolicy(setup)
	if err != nil {
		return nil, err
	}
	t.policy = policy
	session, err := client.NewSession()
	if err != nil {
		return nil, networkError(setup, err)
	}
	// The SSH PTY carries bytes for tmux, including Ctrl+C; the outer tty must
	// not treat them as signals or line editing before tmux starts reading.
	if err = session.RequestPty("xterm-256color", policy.Rows, policy.Cols, ssh.TerminalModes{
		ssh.ECHO: 0, ssh.ICANON: 0, ssh.ISIG: 0, ssh.IEXTEN: 0, ssh.ICRNL: 0, ssh.INLCR: 0, ssh.IGNCR: 0,
		ssh.IXON: 0, ssh.IXOFF: 0, ssh.ISTRIP: 0, ssh.INPCK: 0, ssh.OPOST: 0, ssh.CS8: 1, ssh.PARENB: 0,
	}); err != nil {
		return nil, fail("pty_unavailable")
	}
	t.stdin, err = session.StdinPipe() // only InputTerminal exposes a write path
	if err != nil {
		return nil, fail("pty_unavailable")
	}
	t.reader, err = session.StdoutPipe()
	if err != nil {
		return nil, fail("pty_unavailable")
	}
	session.Stderr = io.Discard
	if err = session.Start(attach); err != nil {
		return nil, fail("terminal_attach_failed")
	}
	reader := bufio.NewReaderSize(t.reader, 256)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > 128 {
		return nil, fail("terminal_attach_failed")
	}
	t.tty = strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	if !tmux.ValidTTY(t.tty) {
		return nil, fail("terminal_attach_failed")
	}
	t.reader = reader
	t.clientCommand, _ = tmux.ClientTargetsCommand(h.Socket)
	for {
		if err = t.checkClient(setup); err == nil {
			break
		}
		if Code(err) != "terminal_target_changed" {
			return nil, err
		}
		select {
		case <-setup.Done():
			return nil, fail("timeout")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err = t.Check(setup); err != nil {
		return nil, err
	}
	if setup.Err() != nil || !stopSetup() {
		return nil, fail("timeout")
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return nil, fail("connection_failed")
	}
	t.stop = context.AfterFunc(ctx, func() { _ = client.Close() })
	ok = true
	return t, nil
}

func (t *terminal) checkClient(ctx context.Context) error {
	output, _, err := run(ctx, t.client, t.clientCommand, tmux.MaxOutput)
	if err != nil {
		return err
	}
	if tmux.CheckClientTarget(output, t.tty, t.target) != nil {
		return fail("terminal_target_changed")
	}
	return nil
}

func (t *terminal) readPolicy(ctx context.Context) (tmux.TerminalPolicy, error) {
	out, _, err := run(ctx, t.client, t.command, tmux.MaxOutput)
	if err != nil {
		return tmux.TerminalPolicy{}, err
	}
	policy, err := tmux.ParseTerminalPolicy(t.host.ID, t.host.Socket, t.target, t.epoch, out)
	if err == tmux.ErrChanged {
		return policy, fail("terminal_target_changed")
	}
	if err != nil {
		return policy, fail("terminal_size_policy")
	}
	return policy, nil
}

func (t *terminal) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = t.client.Close() })
	defer stop()
	policy, err := t.readPolicy(ctx)
	if err != nil {
		return err
	}
	if policy.Signature != t.policy.Signature {
		return fail("terminal_target_changed")
	}
	return t.checkClient(ctx)
}
func (t *terminal) Size() (int, int)           { return t.policy.Cols, t.policy.Rows }
func (t *terminal) Read(p []byte) (int, error) { return t.reader.Read(p) }
func (t *terminal) Close() error {
	var err error
	t.once.Do(func() {
		if t.stop != nil {
			t.stop()
		}
		err = t.client.Close()
	})
	return err
}
