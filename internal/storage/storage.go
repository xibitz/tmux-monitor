package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

const LatestVersion = 8

func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0700); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(dataDir, "hub.sqlite"))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// Serializing operations also prevents unbounded SQLite connection contention.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate runs each numbered migration transactionally. Rollback is explicit
// CLI work; startup only upgrades and never erases an existing database.
func Migrate(ctx context.Context, db *sql.DB, target int) error {
	if target < 0 || target > LatestVersion {
		return fmt.Errorf("unsupported migration target")
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&version); err != nil {
		return err
	}
	if version > LatestVersion {
		return fmt.Errorf("database schema is newer than this binary")
	}
	for version != target {
		number, direction := version+1, "up"
		if target < version {
			number, direction = version, "down"
		}
		matches, globErr := fs.Glob(migrations, fmt.Sprintf("migrations/%03d_*.%s.sql", number, direction))
		if globErr != nil || len(matches) != 1 {
			return fmt.Errorf("expected one migration for version %d %s", number, direction)
		}
		body, readErr := migrations.ReadFile(matches[0])
		if readErr != nil {
			return readErr
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return err
		}
		if direction == "up" {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES (?,?)", number, Now())
			version++
		} else {
			_, err = tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version=?", number)
			version--
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ID(prefix string) string {
	return prefix + hex.EncodeToString(randBytes(16))
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("system randomness unavailable")
	}
	return b
}

// Fixed fractional precision preserves chronological ordering in SQLite TEXT.
func Timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func Now() string                  { return Timestamp(time.Now()) }
