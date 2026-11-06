#!/usr/bin/env python3
"""Disposable local tmux multi-client probe. Never attaches to existing sockets."""
import fcntl
import json
import os
import pathlib
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


def main():
    version = subprocess.check_output(["tmux", "-V"], text=True).strip()
    match = re.fullmatch(r"tmux (\d+)\.(\d+)[a-z]?", version)
    if not match or tuple(map(int, match.groups())) < (3, 7):
        raise RuntimeError("manual sizing spike requires tmux >=3.7; older versions can crash the server")
    with tempfile.TemporaryDirectory(prefix="tmux-monitor-spike-") as directory:
        socket = str(pathlib.Path(directory) / "tmux.sock")
        prefix = ["tmux", "-S", socket, "-f", "/dev/null"]
        processes, descriptors = [], []

        def command(*args):
            return subprocess.check_output(prefix + list(args), text=True).strip()

        def wait_for(condition):
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                if condition():
                    return
                time.sleep(0.05)
            raise RuntimeError("tmux probe timed out")

        def attach(cols, rows, flags):
            master, slave = pty.openpty()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
            attach_args = ["attach-session", "-t", "probe"]
            if flags:
                attach_args += ["-f", flags]
            process = subprocess.Popen(
                prefix + attach_args,
                stdin=slave, stdout=slave, stderr=slave,
                env={**os.environ, "TERM": "xterm-256color"},
                start_new_session=True,
            )
            os.close(slave)
            processes.append(process)
            descriptors.append(master)
            return process, master

        try:
            subprocess.run(prefix + ["new-session", "-d", "-s", "probe", "-x", "120", "-y", "40", "sleep 300"], check=True)
            desktop, _ = attach(120, 40, "")
            wait_for(lambda: len(command("list-clients", "-F", "#{client_name}").splitlines()) == 1)
            original = command("display-message", "-p", "-t", "probe", "#{window_width}x#{window_height}")
            mobile, mobile_fd = attach(40, 20, "read-only,ignore-size")
            wait_for(lambda: len(command("list-clients", "-F", "#{client_name}").splitlines()) == 2)
            flags = command("list-clients", "-F", "#{client_flags}")
            assert "read-only" in flags and "ignore-size" in flags, flags
            after_attach = command("display-message", "-p", "-t", "probe", "#{window_width}x#{window_height}")
            assert after_attach == original, (original, after_attach)
            os.write(mobile_fd, b"\x02c")  # readonly client must ignore new-window
            time.sleep(0.2)
            assert command("list-windows", "-t", "probe", "-F", "#{window_id}").count("\n") == 0
            mobile.terminate()
            mobile.wait(timeout=3)
            assert subprocess.run(prefix + ["has-session", "-t", "probe"], capture_output=True).returncode == 0
            assert command("display-message", "-p", "-t", "probe", "#{window_width}x#{window_height}") == original
            epoch = command("display-message", "-p", "-t", "probe", "#{pid}|#{start_time}|#{socket_path}")
            assert all(epoch.split("|")), epoch
            # Observe basic command blocks only; a full control parser is M4 work.
            control = subprocess.Popen(prefix + ["-C", "attach-session", "-f", "read-only,ignore-size", "-t", "probe"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            processes.append(control)
            control.stdin.write(b"list-sessions\n")
            control.stdin.flush()
            output = b""
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                ready, _, _ = select.select([control.stdout], [], [], 0.2)
                if ready:
                    output += os.read(control.stdout.fileno(), 65536)
                    if b"%begin" in output and b"%end" in output:
                        break
            assert b"%begin" in output and b"%end" in output, output
            control.terminate()
            control.wait(timeout=3)
            desktop.terminate()
            desktop.wait(timeout=3)
            wait_for(lambda: not command("list-clients", "-F", "#{client_name}"))
            # Conservative policy for the first viewer: never alter options on
            # a real host. Here, test manual sizing on our disposable server.
            command("set-option", "-gw", "window-size", "manual")
            command("set-option", "-w", "-t", "probe", "window-size", "manual")
            command("resize-window", "-t", "probe", "-x", "120", "-y", "39")
            command("new-window", "-d", "-t", "probe", "-n", "second", "sleep 300")
            command("resize-window", "-t", "probe:1", "-x", "90", "-y", "30")

            def sizes():
                return command("list-windows", "-t", "probe", "-F", "#{window_id}|#{window_width}|#{window_height}|#{window-size}")

            before = sizes()
            standalone, standalone_fd = attach(40, 20, "read-only,ignore-size")
            wait_for(lambda: len(command("list-clients", "-F", "#{client_name}").splitlines()) == 1)
            assert sizes() == before, (before, sizes())
            fcntl.ioctl(standalone_fd, termios.TIOCSWINSZ, struct.pack("HHHH", 10, 25, 0, 0))
            os.kill(standalone.pid, signal.SIGWINCH)
            command("select-window", "-t", "probe:1")
            time.sleep(0.2)
            assert sizes() == before, (before, sizes())
            standalone.terminate()
            standalone.wait(timeout=3)
            assert sizes() == before, (before, sizes())
            assert subprocess.run(prefix + ["has-session", "-t", "probe"], capture_output=True).returncode == 0
            print(json.dumps({"tmux": subprocess.check_output(["tmux", "-V"], text=True).strip(), "os": os.uname().sysname, "desktopSize": original, "mobileSize": "40x20", "sizePreserved": True, "readOnlyBlocksNewWindow": True, "sessionSurvivesClientDisconnect": True, "epochFieldsPresent": True, "basicControlBlocks": True, "manualSizingWithoutDesktop": True, "mixedWindowSizesPreserved": True, "ptyResizePreservesManualWindows": True}, indent=2))
        finally:
            for process in processes:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=2)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
            for descriptor in descriptors:
                os.close(descriptor)
            # Cleanup is restricted to the disposable socket created above.
            subprocess.run(prefix + ["kill-server"], capture_output=True)


if __name__ == "__main__":
    main()
