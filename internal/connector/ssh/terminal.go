package sshconnector

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/xibitz/tmux-monitor/internal/tmux"
	"golang.org/x/crypto/ssh"
)

// Terminal intentionally offers no Write or Resize method in this slice.
type Terminal interface {
	io.Reader
	io.Closer
	Size() (int, int)
	Check(context.Context) error
}

type terminal struct {
	client                 *ssh.Client
	reader                 io.Reader
	stdin                  io.WriteCloser
	policy                 tmux.TerminalPolicy
	host                   Host
	target, epoch, command string
	once                   sync.Once
	stop                   func() bool
}

func (m *Manager) OpenTerminal(ctx context.Context, h Host, record Record, target, epoch string) (Terminal, error) {
	command, err := tmux.TerminalPolicyCommand(h.Socket, target)
	if err != nil {
		return nil, fail("terminal_target_invalid")
	}
	attach, err := tmux.AttachCommand(h.Socket, target)
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
	if err = session.RequestPty("xterm-256color", policy.Rows, policy.Cols, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
		return nil, fail("pty_unavailable")
	}
	t.stdin, err = session.StdinPipe() // held open; gateway has no path to write
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
	return nil
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
