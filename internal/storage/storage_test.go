package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationRollbackReapplyAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, version := range []int{1, 1, 0, 1} {
		if err := Migrate(ctx, db, version); err != nil {
			t.Fatal(err)
		}
		var got int
		if err := db.QueryRow("SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&got); err != nil || got != version {
			t.Fatalf("version %d: %d %v", version, got, err)
		}
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL: %s %v", mode, err)
	}
	var keys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&keys); err != nil || keys != 1 {
		t.Fatalf("foreign keys: %d %v", keys, err)
	}
	for _, check := range []struct {
		path string
		mode os.FileMode
	}{{dir, 0700}, {filepath.Join(dir, "hub.sqlite"), 0600}} {
		info, err := os.Stat(check.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != check.mode {
			t.Fatalf("permissions: %s %o", check.path, info.Mode().Perm())
		}
	}
	if _, err := db.Exec("INSERT INTO app_sessions(id_hash,user_id,expires_at,created_at) VALUES ('x','absent','x','x')"); err == nil {
		t.Fatal("foreign key violation accepted")
	}
	if _, err := db.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES (99,'future')"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, 1); err == nil {
		t.Fatal("accepted newer database schema")
	}
}
