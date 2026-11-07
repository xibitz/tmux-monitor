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
	for _, version := range []int{1, 2, 3, 4, 4, 3, 2, 1, 0, 4} {
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

func TestTerminalAuditMigrationPreservesExistingAudit(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = Migrate(ctx, db, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO audit_events(actor,action,result,request_id,at) VALUES ('admin','existing','success','req_test',?)", Now()); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{3, 2, 3} {
		if err = Migrate(ctx, db, version); err != nil {
			t.Fatal(err)
		}
		var action string
		if err = db.QueryRow("SELECT action FROM audit_events").Scan(&action); err != nil || action != "existing" {
			t.Fatal("audit history lost")
		}
	}
	var target string
	if err = db.QueryRow("SELECT target FROM audit_events").Scan(&target); err != nil || target != "" {
		t.Fatal("legacy audit default invalid")
	}
}

func TestOperationMigrationPreservesAuditAndExplicitRollback(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = Migrate(ctx, db, 3); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO audit_events(actor,action,result,request_id,at) VALUES ('test','prior','success','req_test',?)`, Now()); err != nil {
		t.Fatal(err)
	}
	for _, v := range []int{4, 3, 4} {
		if err = Migrate(ctx, db, v); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM audit_events WHERE action='prior'`).Scan(&count); err != nil || count != 1 {
			t.Fatal("prior audit lost")
		}
	}
	if _, err = db.Exec(`INSERT INTO tmux_operations(actor_id,key_hash,request_hash,operation_id,host_id,state,created_at,updated_at) VALUES ('test','hash','request','op_test','host','pending',?,?)`, Now(), Now()); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db, 4); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = db.QueryRow(`SELECT state FROM tmux_operations WHERE operation_id='op_test'`).Scan(&state); err != nil || state != "pending" {
		t.Fatal("startup lost durable reservation")
	}
}

func TestSSHMigrationPreservesFoundationData(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := Migrate(ctx, db, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO hosts(id,label,transport,os_user,socket_name,created_at,updated_at) VALUES ('existing','Existing','ssh','dev','default',?,?)", Now(), Now()); err != nil {
		t.Fatal(err)
	}
	for _, target := range []int{2, 1, 2} {
		if err := Migrate(ctx, db, target); err != nil {
			t.Fatal(err)
		}
		var label string
		if err := db.QueryRow("SELECT label FROM hosts WHERE id='existing'").Scan(&label); err != nil || label != "Existing" {
			t.Fatal("foundation data changed by migration 002")
		}
	}
}
