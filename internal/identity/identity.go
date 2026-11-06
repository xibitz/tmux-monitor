package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xibitz/tmux-monitor/internal/storage"
	"golang.org/x/crypto/argon2"
)

const SessionLifetime = 12 * time.Hour
const CookieName = "tmux_monitor_session"

var ErrCredentials = errors.New("invalid credentials")

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
}

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", fmt.Errorf("password must contain 12–1024 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return "$argon2id$v=19$m=65536,t=3,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyHash costs the same as a real user's hash to avoid a cheap unknown-user path.
const DummyHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func Bootstrap(ctx context.Context, db *sql.DB, username, password string) error {
	if len(username) < 1 || len(username) > 64 || strings.TrimSpace(username) != username {
		return fmt.Errorf("username must contain 1–64 characters without surrounding whitespace")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("bootstrap is disabled: a user already exists")
	}
	id := storage.ID("u_")
	if _, err = tx.ExecContext(ctx, "INSERT INTO users(id,username,password_hash,is_admin,created_at) VALUES (?,?,?,1,?)", id, username, hash, storage.Now()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_events(actor,action,result,request_id,at) VALUES (?,'admin.bootstrap','success','local-cli',?)", id, storage.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func Login(ctx context.Context, db *sql.DB, username, password string) (User, string, error) {
	var user User
	var hash string
	err := db.QueryRowContext(ctx, "SELECT id,username,is_admin,password_hash FROM users WHERE username=? AND disabled_at IS NULL", username).Scan(&user.ID, &user.Username, &user.IsAdmin, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", err
	}
	if errors.Is(err, sql.ErrNoRows) {
		hash = DummyHash
	}
	valid := VerifyPassword(hash, password)
	if !valid || user.ID == "" {
		return User{}, "", ErrCredentials
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return User{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM app_sessions WHERE expires_at <= ?", storage.Now()); err != nil {
		return User{}, "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO app_sessions(id_hash,user_id,expires_at,created_at) VALUES (?,?,?,?)", TokenHash(token), user.ID, storage.Timestamp(time.Now().Add(SessionLifetime)), storage.Now()); err != nil {
		return User{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

func Authenticate(ctx context.Context, db *sql.DB, token string) (User, error) {
	var user User
	if len(token) != 43 {
		return user, ErrCredentials
	}
	err := db.QueryRowContext(ctx, `SELECT u.id,u.username,u.is_admin FROM app_sessions s JOIN users u ON u.id=s.user_id WHERE s.id_hash=? AND s.expires_at>? AND u.disabled_at IS NULL`, TokenHash(token), storage.Now()).Scan(&user.ID, &user.Username, &user.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return user, ErrCredentials
	}
	return user, err
}

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func CSRF(token string) string {
	h := hmac.New(sha256.New, []byte(token))
	h.Write([]byte("tmux-monitor:csrf:v1"))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
