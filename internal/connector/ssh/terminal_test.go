package sshconnector

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/xibitz/tmux-monitor/internal/tmux"
	"golang.org/x/crypto/ssh"
)

// PTY-aware SSH peer, executing only this test's fixed catalog on its own socket.
func terminalServer(t *testing.T, f *fixture, env []string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(key.Marshal(), f.clientSigner.PublicKey().Marshal()) {
			return nil, nil
		}
		return nil, ErrCredentials
	}}
	cfg.AddHostKey(f.hostSigner)
	policy, _ := tmux.TerminalPolicyCommand(f.host.Socket, "$0")
	attach, _ := tmux.AttachCommand(f.host.Socket, "$0")
	var wg sync.WaitGroup
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			raw, e := l.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			connections[raw] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer raw.Close()
				defer func() { mu.Lock(); delete(connections, raw); mu.Unlock() }()
				_, channels, requests, e := ssh.NewServerConn(raw, cfg)
				if e != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					if ch.ChannelType() != "session" {
						_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					channel, requests, e := ch.Accept()
					if e != nil {
						return
					}
					wg.Add(1)
					go func() {
						defer wg.Done()
						defer channel.Close()
						size := pty.Winsize{Cols: 80, Rows: 24}
						hasPTY := false
						for request := range requests {
							switch request.Type {
							case "pty-req":
								var p struct {
									Term                      string
									Cols, Rows, Width, Height uint32
									Modes                     string
								}
								if ssh.Unmarshal(request.Payload, &p) != nil {
									_ = request.Reply(false, nil)
									continue
								}
								size = pty.Winsize{Cols: uint16(p.Cols), Rows: uint16(p.Rows)}
								hasPTY = true
								_ = request.Reply(true, nil)
							case "exec":
								var x struct{ Command string }
								if ssh.Unmarshal(request.Payload, &x) != nil {
									return
								}
								if x.Command == "LC_ALL=C id -un" {
									_ = request.Reply(true, nil)
									_, _ = io.WriteString(channel, "tester\n")
									_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
									return
								}
								if x.Command != policy && x.Command != attach {
									_ = request.Reply(false, nil)
									return
								}
								if x.Command == attach && !hasPTY {
									_ = request.Reply(false, nil)
									return
								}
								_ = request.Reply(true, nil)
								cmd := exec.Command("/bin/sh", "-c", x.Command)
								cmd.Env = env
								if x.Command == policy {
									var stderr bytes.Buffer
									cmd.Stderr = &stderr
									out, e := cmd.Output()
									_, _ = channel.Write(out)
									_, _ = channel.Stderr().Write(stderr.Bytes())
									status := uint32(0)
									if e != nil {
										status = 1
									}
									_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
									return
								}
								master, e := pty.StartWithSize(cmd, &size)
								if e != nil {
									return
								}
								done := make(chan struct{})
								go func() { _, _ = io.Copy(master, channel); _ = cmd.Process.Kill(); close(done) }()
								_, _ = io.Copy(channel, master)
								_ = cmd.Wait()
								_ = master.Close()
								_ = channel.Close()
								<-done
								return
							default:
								_ = request.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		for c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return l.Addr().String()
}

func TestReadOnlyTerminalUsesRealSSHPTYAndPreservesSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	version, err := exec.Command("tmux", "-V").Output()
	if err != nil || !tmux.TerminalVersionSupported(strings.TrimPrefix(strings.TrimSpace(string(version)), "tmux ")) {
		t.Skip("manual sizing integration requires tmux >=3.7; CI pins a supported release")
	}
	f := setup(t)
	f.host.Socket = "terminal-test"
	dir, err := os.MkdirTemp("/tmp", "mtm-pty-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	env := append(os.Environ(), "TMUX_TMPDIR="+dir, "TMUX=", "TERM=xterm-256color")
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("tmux", append([]string{"-L", f.host.Socket, "-f", "/dev/null"}, args...)...)
		cmd.Env = env
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("test tmux: %v %s", e, out)
		}
		return out
	}
	t.Cleanup(func() { cmd := exec.Command("tmux", "-L", f.host.Socket, "kill-server"); cmd.Env = env; _ = cmd.Run() })
	run("new-session", "-d", "-s", "terminal", "-x", "120", "-y", "39", "printf 'terminal-fixture-ready'; sleep 120")
	m := manager(f, terminalServer(t, f, env))
	fingerprint := strings.TrimSpace(string(run("display-message", "-p", "F|#{pid}|#{start_time}|#{q:socket_path}|#{version}")))
	inv, e := tmux.Parse(f.host.ID, f.host.Socket, "3.8", []byte(fingerprint+"\n"+fingerprint+"\n"))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if stream, e := m.OpenTerminal(ctx, f.host, f.record, "$0", inv.Epoch); Code(e) != "terminal_size_policy" {
		if stream != nil {
			_ = stream.Close()
		}
		t.Fatalf("unsafe automatic sizing accepted: %v", e)
	}
	run("set-option", "-gw", "window-size", "manual")
	run("set-option", "-w", "-t", "$0", "window-size", "manual")
	run("new-window", "-d", "-t", "$0", "-n", "other", "sleep 120")
	run("resize-window", "-t", "@1", "-x", "90", "-y", "30")
	before := string(run("list-windows", "-t", "$0", "-F", "#{window_id}|#{window_width}|#{window_height}"))
	stream, e := m.OpenTerminal(ctx, f.host, f.record, "$0", inv.Epoch)
	if e != nil {
		t.Fatal(e)
	}
	cols, rows := stream.Size()
	if cols != 120 || rows != 40 {
		t.Fatalf("grid %dx%d", cols, rows)
	}
	read := make(chan error, 1)
	go func() {
		var received []byte
		buf := make([]byte, 4096)
		for {
			n, e := stream.Read(buf)
			received = append(received, buf[:n]...)
			if bytes.Contains(received, []byte("terminal-fixture-ready")) {
				read <- nil
				return
			}
			if e != nil {
				read <- e
				return
			}
		}
	}()
	select {
	case e := <-read:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("PTY output timeout")
	}
	flags := string(run("list-clients", "-F", "#{client_flags}"))
	if !strings.Contains(flags, "read-only") || !strings.Contains(flags, "ignore-size") {
		t.Fatal("missing tmux protection")
	}
	if after := string(run("list-windows", "-t", "$0", "-F", "#{window_id}|#{window_width}|#{window_height}")); after != before {
		t.Fatal("attach resized windows")
	}
	if e := stream.Check(ctx); e != nil {
		t.Fatal(e)
	}
	run("set-option", "-w", "-t", "@1", "window-size", "latest")
	if e := stream.Check(ctx); Code(e) != "terminal_size_policy" {
		t.Fatalf("policy change accepted: %v", e)
	}
	_ = stream.Close()
	run("has-session", "-t", "$0")
	deadline := time.Now().Add(2 * time.Second)
	for strings.TrimSpace(string(run("list-clients", "-F", "#{client_name}"))) != "" {
		if time.Now().After(deadline) {
			t.Fatal("viewer not detached")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stream, e := m.OpenTerminal(ctx, f.host, f.record, "$0", "e_"+strings.Repeat("0", 64)); Code(e) != "terminal_target_changed" {
		if stream != nil {
			_ = stream.Close()
		}
		t.Fatal("stale epoch accepted")
	}
}
