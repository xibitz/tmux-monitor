package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/xibitz/tmux-monitor/internal/agentproto"
	"github.com/xibitz/tmux-monitor/internal/tmux"
)

type limitedOutput struct {
	buf      bytes.Buffer
	max      int
	exceeded bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.max-b.buf.Len() {
		b.exceeded = true
		return 0, io.ErrShortBuffer
	}
	return b.buf.Write(p)
}

func localTmux(ctx context.Context, limit int, args ...string) ([]byte, string, string) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.WaitDelay = time.Second
	out, stderr := &limitedOutput{max: limit}, &limitedOutput{max: 16 * 1024}
	cmd.Stdout, cmd.Stderr = out, stderr
	err := cmd.Run()
	switch {
	case out.exceeded || stderr.exceeded:
		return nil, "", "output_limit"
	case ctx.Err() != nil:
		return nil, "", "timeout"
	case errors.Is(err, exec.ErrNotFound):
		return nil, "", "tmux_missing"
	case err != nil:
		return nil, strings.TrimSpace(stderr.buf.String()), "command_failed"
	default:
		return out.buf.Bytes(), "", "ok"
	}
}

func tmuxVersion(ctx context.Context) (string, string) {
	data, _, status := localTmux(ctx, 1024, "-V")
	if status != "ok" {
		return "", status
	}
	version, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "tmux ")
	if !ok || !agentproto.ValidTmuxVersion(version) {
		return "", "tmux_missing"
	}
	return version, "ok"
}

func snapshot(ctx context.Context, socket string) agentproto.SnapshotResult {
	version, status := tmuxVersion(ctx)
	if status != "ok" {
		return agentproto.SnapshotResult{Status: status}
	}
	args, err := tmux.SnapshotArgs(socket, false)
	if err != nil {
		return agentproto.SnapshotResult{Status: "inventory_invalid"}
	}
	// tmux receives a fixed argv catalog. Global -N avoids creating a server.
	out, stderr, status := localTmux(ctx, tmux.MaxOutput, append([]string{"-N"}, args[1:]...)...)
	if status == "command_failed" && stderr == "no current target" {
		args, err = tmux.SnapshotArgs(socket, true)
		if err != nil {
			return agentproto.SnapshotResult{Status: "inventory_invalid"}
		}
		out, stderr, status = localTmux(ctx, tmux.MaxOutput, append([]string{"-N"}, args[1:]...)...)
	}
	if status == "command_failed" && (strings.HasPrefix(stderr, "no server running on ") ||
		strings.HasPrefix(stderr, "error connecting to ") && strings.Contains(stderr, "(No such file or directory)")) {
		status = "no_tmux_server"
	}
	return agentproto.SnapshotResult{Status: status, Version: version, Output: out}
}
