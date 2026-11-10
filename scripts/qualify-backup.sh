#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
lab=$(mktemp -d "${TMPDIR:-/tmp}/mtm-backup-qualify.XXXXXX")
trap 'rm -rf "$lab"' EXIT
export TMUX_MONITOR_DATA_DIR="$lab/data"
age --version
printf '%s\n' 'fixture-backup-password-only' | ./bin/hub bootstrap --password-stdin
python3 - "$lab/data/hub.sqlite" <<'PY'
import sqlite3,sys
with sqlite3.connect(sys.argv[1]) as db:
    db.execute("INSERT INTO hosts(id,label,transport,tailnet_address,os_user,socket_name,created_at,updated_at) VALUES('h_fixture','Backup fixture','ssh','fixture.integration.ts.net','tester','fixture','now','now')")
PY
ssh-keygen -q -t ed25519 -N '' -f "$lab/client"
ssh-keygen -q -t ed25519 -N '' -f "$lab/host"
fp=$(ssh-keygen -lf "$lab/host.pub" -E sha256 | awk '{print $2}')
./bin/hub ssh-configure --host-id h_fixture --key-file "$lab/client" --host-key-file "$lab/host.pub" --expected-fingerprint "$fp"
age-keygen -o "$lab/db.agekey" 2>/dev/null
age-keygen -o "$lab/keys.agekey" 2>/dev/null
db_recipient=$(age-keygen -y "$lab/db.agekey")
key_recipient=$(age-keygen -y "$lab/keys.agekey")
python3 scripts/hub-backup.py backup --data-dir "$lab/data" --output "$lab/backup" --db-recipient "$db_recipient" --secrets-recipient "$key_recipient"
python3 scripts/hub-backup.py restore --backup-dir "$lab/backup" --db-identity "$lab/db.agekey" --secrets-identity "$lab/keys.agekey" --output "$lab/restore"
python3 - "$lab" <<'PY'
from pathlib import Path
import sqlite3, stat, sys
lab=Path(sys.argv[1])
data=lab/"restore"
with sqlite3.connect(data/"hub.sqlite") as db:
    assert db.execute("PRAGMA quick_check").fetchone()==("ok",)
    assert db.execute("SELECT max(version) FROM schema_migrations").fetchone()==(8,)
    assert db.execute("SELECT username FROM users").fetchone()==("admin",)
    assert db.execute("SELECT label FROM hosts").fetchone()==("Backup fixture",)
assert stat.S_IMODE(data.stat().st_mode)==0o700
for key in (lab/"data"/"secrets"/"ssh").iterdir():
    recovered=data/"secrets"/"ssh"/key.name
    assert recovered.read_bytes()==key.read_bytes()
    assert stat.S_IMODE(recovered.stat().st_mode)==0o600
PY
if python3 scripts/hub-backup.py restore --backup-dir "$lab/backup" --db-identity "$lab/keys.agekey" --secrets-identity "$lab/db.agekey" --output "$lab/bad" 2>"$lab/rejected"; then
  echo 'Restore accepted wrong identity'; exit 1
fi
test ! -e "$lab/bad"
if python3 scripts/hub-backup.py restore --backup-dir "$lab/backup" --db-identity "$lab/db.agekey" --secrets-identity "$lab/keys.agekey" --output "$lab/restore" 2>"$lab/rejected"; then
  echo 'Restore overwrote an existing directory'; exit 1
fi
python3 - "$lab/data/hub.sqlite" <<'PY'
import sqlite3,sys
with sqlite3.connect(sys.argv[1]) as db:
    db.execute("UPDATE hosts SET label='Second snapshot'")
PY
python3 scripts/hub-backup.py backup --data-dir "$lab/data" --output "$lab/other" --db-recipient "$db_recipient" --secrets-recipient "$key_recipient"
cp "$lab/other/ssh-secrets.json.age" "$lab/backup/ssh-secrets.json.age"
if python3 scripts/hub-backup.py restore --backup-dir "$lab/backup" --db-identity "$lab/db.agekey" --secrets-identity "$lab/keys.agekey" --output "$lab/mixed" 2>"$lab/rejected"; then
  echo 'Restore accepted mismatched artifacts'; exit 1
fi
test ! -e "$lab/mixed"
TMUX_MONITOR_DATA_DIR="$lab/restore" ./bin/hub migrate --to 7 --allow-data-loss
TMUX_MONITOR_DATA_DIR="$lab/restore" ./bin/hub migrate --to 8
echo 'PASS encrypted SQLite+SSH key backup, restore, permissions, identity/mismatch rejection and migration 8→7→8'
