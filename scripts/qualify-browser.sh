#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
lab=$(mktemp -d "${TMPDIR:-/tmp}/mtm-browser-qualify.XXXXXX")
trap 'if [[ -n "${fixture_pid:-}" ]]; then kill -TERM "$fixture_pid" 2>/dev/null || true; wait "$fixture_pid" || true; fi; rm -rf "$lab"' EXIT
openssl req -x509 -nodes -newkey rsa:2048 -keyout "$lab/tls.key" -out "$lab/tls.crt" -days 1 -subj '/CN=localhost' -addext 'subjectAltName=DNS:localhost' >/dev/null 2>&1
go test -c -o "$lab/fixture" ./internal/httpapi
TMUX_MONITOR_BROWSER_FIXTURE=1 TMUX_MONITOR_BROWSER_QUALIFICATION=1 TMUX_MONITOR_BROWSER_CERT="$lab/tls.crt" TMUX_MONITOR_BROWSER_KEY="$lab/tls.key" "$lab/fixture" -test.run '^TestBrowserFixture$' -test.v > "$lab/server.log" 2>&1 &
fixture_pid=$!
ready=0
for attempt in {1..30}; do
  if ! kill -0 "$fixture_pid" 2>/dev/null; then break; fi
  if curl -fkSs https://localhost:18081/healthz >/dev/null 2>&1; then ready=1; break; fi
  sleep 0.2
done
if [[ "$ready" != 1 ]]; then cat "$lab/server.log"; exit 1; fi
node scripts/qualify-browser.mjs || { cat "$lab/server.log"; exit 1; }
