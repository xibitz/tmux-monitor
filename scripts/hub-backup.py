#!/usr/bin/env python3
"""Create encrypted hub backups, or restore them into a new directory."""
import argparse
import base64
import contextlib
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import sqlite3
import stat
import subprocess
import tempfile

KEY_REF = re.compile(r"k_[a-f0-9]{32}")
MAX_KEY = 64 * 1024
MAX_KEYS = 16 * 1024 * 1024
MAX_SECRET_PAYLOAD = 23 * 1024 * 1024
MAX_ENCRYPTED_KEYS = 24 * 1024 * 1024
DATABASE = "hub.sqlite.age"
SECRETS = "ssh-secrets.json.age"


def regular_file(path, max_size=None):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or (max_size is not None and info.st_size > max_size):
        raise ValueError("expected a regular file within the supported size limit")
    return info


def key_references(database, check_integrity=False):
    regular_file(database)
    with contextlib.closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)) as db:
        db.execute("PRAGMA trusted_schema=OFF")
        if check_integrity and db.execute("PRAGMA quick_check").fetchall() != [("ok",)]:
            raise ValueError("restored SQLite snapshot failed its integrity check")
        (schema,) = db.execute("SELECT COALESCE(MAX(version),0) FROM schema_migrations").fetchone()
        if schema == 1:
            return set()
        if schema < 1:
            raise ValueError("database does not contain an installed hub schema")
        refs = set()
        for (ref,) in db.execute("SELECT key_ref FROM ssh_credentials"):
            if not isinstance(ref, str) or not KEY_REF.fullmatch(ref):
                raise ValueError("SQLite contains an invalid SSH key reference")
            refs.add(ref)
        return refs


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def age(options, output, data=None):
    with output.open("xb") as file:
        subprocess.run(["age", *options], input=data, stdout=file, check=True)


@contextlib.contextmanager
def new_directory(path):
    path.mkdir(mode=0o700)
    try:
        yield
    except BaseException:
        shutil.rmtree(path)
        raise


def copy_database(source, snapshot):
    regular_file(source)
    with contextlib.closing(sqlite3.connect(source.as_uri() + "?mode=ro", uri=True)) as live:
        with contextlib.closing(sqlite3.connect(snapshot)) as copy:
            live.backup(copy)


def export_keys(source, refs):
    if not refs:
        return {}
    for path in (source / "secrets", source / "secrets" / "ssh"):
        if not stat.S_ISDIR(path.lstat().st_mode):
            raise ValueError("SSH secret directories must be real directories")
    keys = {}
    size = 0
    for ref in sorted(refs):
        key = source / "secrets" / "ssh" / ref
        info = regular_file(key, MAX_KEY)
        if stat.S_IMODE(info.st_mode) != 0o600 or info.st_size == 0:
            raise ValueError("referenced SSH key must be a nonempty 0600 file")
        size += info.st_size
        if size > MAX_KEYS:
            raise ValueError("SSH key store exceeds the backup limit")
        keys[ref] = base64.b64encode(key.read_bytes()).decode("ascii")
    return keys


def backup(args):
    for recipient in (args.db_recipient, args.secrets_recipient):
        if not re.fullmatch(r"age1[0-9a-z]+", recipient):
            raise ValueError("use a native age recipient for each encrypted file")
    source = args.data_dir.resolve(strict=True)
    output = args.output.absolute()
    if output.resolve().is_relative_to(source):
        raise ValueError("choose a backup directory outside the hub data directory")
    with new_directory(output):
        with tempfile.TemporaryDirectory(dir=output) as temp:
            snapshot = Path(temp) / "hub.sqlite"
            copy_database(source / "hub.sqlite", snapshot)
            keys = export_keys(source, key_references(snapshot))
            secret_data = json.dumps({"dbSha256": sha256(snapshot), "keys": keys}).encode()
            if len(secret_data) > MAX_SECRET_PAYLOAD:
                raise ValueError("serialized SSH key store exceeds the backup limit")
            age(["-r", args.db_recipient, str(snapshot)], output / DATABASE)
            age(["-r", args.secrets_recipient], output / SECRETS, secret_data)


def restore(args):
    backup_dir = args.backup_dir.resolve(strict=True)
    database, secrets = backup_dir / DATABASE, backup_dir / SECRETS
    regular_file(database)
    regular_file(secrets, MAX_ENCRYPTED_KEYS)
    for identity in (args.db_identity, args.secrets_identity):
        regular_file(identity)
    output = args.output.absolute()
    if output.resolve().is_relative_to(backup_dir):
        raise ValueError("choose a restore directory outside the backup directory")
    with new_directory(output):
        snapshot = output / "hub.sqlite"
        age(["-d", "-i", str(args.db_identity.absolute()), str(database)], snapshot)
        refs = key_references(snapshot, check_integrity=True)
        secret_data = subprocess.run(
            ["age", "-d", "-i", str(args.secrets_identity.absolute()), str(secrets)],
            stdout=subprocess.PIPE,
            check=True,
        ).stdout
        if len(secret_data) > MAX_SECRET_PAYLOAD:
            raise ValueError("serialized SSH key store exceeds the restore limit")
        payload = json.loads(secret_data)
        if not isinstance(payload, dict) or payload.get("dbSha256") != sha256(snapshot):
            raise ValueError("SQLite and SSH secret files are not from the same backup")
        keys = payload.get("keys")
        if not isinstance(keys, dict) or set(keys) != refs:
            raise ValueError("SSH secret references do not match the SQLite snapshot")
        if refs:
            (output / "secrets" / "ssh").mkdir(parents=True, mode=0o700)
        size = 0
        for ref in sorted(refs):
            key = base64.b64decode(keys[ref], validate=True)
            size += len(key)
            if not 0 < len(key) <= MAX_KEY or size > MAX_KEYS:
                raise ValueError("restored SSH key exceeds the supported size limit")
            with (output / "secrets" / "ssh" / ref).open("xb") as file:
                file.write(key)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    create = commands.add_parser("backup", help="back up a stopped hub")
    create.add_argument("--data-dir", type=Path, required=True)
    create.add_argument("--db-recipient", required=True)
    create.add_argument("--secrets-recipient", required=True)
    recover = commands.add_parser("restore", help="restore without overwriting existing data")
    recover.add_argument("--backup-dir", type=Path, required=True)
    recover.add_argument("--db-identity", type=Path, required=True)
    recover.add_argument("--secrets-identity", type=Path, required=True)
    for command in (create, recover):
        command.add_argument("--output", type=Path, required=True, help="new directory")
    args = parser.parse_args()
    os.umask(0o077)
    if shutil.which("age") is None:
        parser.exit(1, "age is required\n")
    try:
        (backup if args.action == "backup" else restore)(args)
    except subprocess.CalledProcessError:
        parser.exit(1, "age failed; check the recipient, identity or encrypted file\n")
    except (OSError, ValueError, TypeError, sqlite3.Error) as err:
        parser.exit(1, f"{args.action} failed: {err}\n")
    print(f"{args.action} completed in {args.output}")


if __name__ == "__main__":
    main()
