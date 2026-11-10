package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/connector/agent"
	"golang.org/x/term"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agent:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 0 && args[0] == "notify" {
		return agent.NotifyHook(ctx, args[1:], os.Stdin)
	}
	if len(args) == 0 || (args[0] != "enroll" && args[0] != "run") {
		return errors.New("usage: agent [enroll|run] --credential-file PATH")
	}
	if os.Geteuid() == 0 || os.Getuid() != os.Geteuid() || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		return errors.New("run as the non-root tmux owner on Linux or macOS")
	}
	account, err := user.Current()
	if err != nil || !agentproto.ValidName(account.Username) {
		return errors.New("cannot determine a supported local username")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	path := flags.String("credential-file", "", "owner-only credential file")
	if args[0] == "run" {
		controlEvents := flags.Bool("control-events", false, "watch tmux control-mode inventory changes")
		hookSocket := flags.String("hook-socket", "", "absolute local notification socket in an owner-only directory")
		enableTerminal := flags.Bool("terminal", false, "allow local tmux terminal viewers from the paired hub")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *path == "" || flags.NArg() != 0 {
			return errors.New("--credential-file is required")
		}
		binding, err := readBinding(*path)
		if err != nil {
			return err
		}
		if binding.OSUser != account.Username {
			return errors.New("local OS user differs from the enrolled profile")
		}
		if *hookSocket != "" {
			if err := agent.ValidateHookPath(*hookSocket); err != nil {
				return err
			}
		}
		return binding.Run(ctx, runtime.GOOS, agent.RunOptions{
			Terminal: *enableTerminal, ControlEvents: *controlEvents, HookSocket: *hookSocket,
		})
	}

	hub := flags.String("hub", "", "HTTPS Tailscale Serve origin")
	host := flags.String("host-id", "", "Agent host profile ID")
	socket := flags.String("socket", "default", "local tmux socket name")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *path == "" || !agentproto.ValidHostID(*host) || !agentproto.ValidName(*socket) || flags.NArg() != 0 {
		return errors.New("--credential-file, --host-id, and a valid --socket are required")
	}
	if _, err := agent.ValidateHub(*hub); err != nil {
		return err
	}
	if _, err := os.Lstat(*path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("credential file must be a new path")
	}
	temp, err := os.CreateTemp(filepath.Dir(*path), ".tmux-monitor-agent-")
	if err != nil {
		return errors.New("cannot create a private credential file in the destination directory")
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0600); err != nil {
		return errors.New("cannot protect credential file")
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("enrollment token must be entered in a terminal")
	}
	fmt.Fprint(os.Stderr, "Enrollment token: ")
	secret, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return errors.New("cannot read enrollment token")
	}
	defer clear(secret)
	binding, err := agent.Enroll(ctx, *hub, strings.TrimSpace(string(secret)),
		agentproto.EnrollRequest{HostID: *host, OSUser: account.Username, Socket: *socket})
	if err != nil {
		return err
	}
	if err := saveBinding(temp, *path, binding); err != nil {
		return errors.New("credential not saved; revoke the enrolled identity on the hub before pairing again")
	}
	fmt.Println("Agent paired. Start it with the run command.")
	return nil
}

func saveBinding(temp *os.File, path string, binding agent.Binding) error {
	if err := json.NewEncoder(temp).Encode(binding); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Link(temp.Name(), path)
}

func readBinding(path string) (agent.Binding, error) {
	var binding agent.Binding
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return binding, errors.New("credential must be a regular 0600 file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return binding, errors.New("cannot open credential file")
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) || current.Mode().Perm() != 0600 {
		return binding, errors.New("credential file changed while opening")
	}
	owner, ok := current.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return binding, errors.New("credential file must belong to the current OS user")
	}
	data, err := io.ReadAll(io.LimitReader(f, agentproto.ControlLimit+1))
	defer clear(data)
	if err != nil || len(data) > agentproto.ControlLimit || agentproto.Decode(data, &binding) != nil ||
		!agentproto.ValidHostID(binding.HostID) || !agentproto.ValidAgentID(binding.AgentID) ||
		!agentproto.ValidSecret(binding.Credential) || !agentproto.ValidName(binding.Socket) ||
		!agentproto.ValidName(binding.OSUser) {
		return agent.Binding{}, errors.New("invalid credential file")
	}
	return binding, nil
}
