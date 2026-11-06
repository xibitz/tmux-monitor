package sshconnector

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/xibitz/tmux-monitor/internal/storage"
	"golang.org/x/crypto/ssh"
)

var ErrRequired = errors.New("credentials_required")
var ErrCredentials = errors.New("credentials_invalid")
var ownedRef = regexp.MustCompile(`^k_[a-f0-9]{32}$`)

type Metadata struct {
	Configured   bool   `json:"configured"`
	Port         int    `json:"port,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	ConfiguredAt string `json:"configuredAt,omitempty"`
}
type Record struct {
	Metadata
	KeyRef    string `json:"-"`
	PublicKey string `json:"-"`
}
type Store struct {
	DB      *sql.DB
	DataDir string
}

func (s Store) Load(ctx context.Context, hostID string) (Record, error) {
	var r Record
	err := s.DB.QueryRowContext(ctx, "SELECT port,key_ref,host_public_key,host_key_fingerprint,configured_at FROM ssh_credentials WHERE host_id=?", hostID).Scan(&r.Port, &r.KeyRef, &r.PublicKey, &r.Fingerprint, &r.ConfiguredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrRequired
	}
	if err != nil {
		return r, err
	}
	r.Configured = true
	return r, nil
}

func (s Store) Key(r Record) (ssh.Signer, ssh.PublicKey, error) {
	path, err := s.path(r.KeyRef)
	if err != nil {
		return nil, nil, ErrCredentials
	}
	body, err := readFile(path, true)
	if err != nil {
		return nil, nil, ErrCredentials
	}
	defer clear(body)
	signer, err := ssh.ParsePrivateKey(body)
	if err != nil {
		return nil, nil, ErrCredentials
	}
	pub, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(r.PublicKey))
	if err != nil || len(bytes.TrimSpace(rest)) != 0 || ssh.FingerprintSHA256(pub) != r.Fingerprint {
		return nil, nil, ErrCredentials
	}
	if len(hostAlgorithms(pub)) == 0 || len(hostAlgorithms(signer.PublicKey())) == 0 {
		return nil, nil, ErrCredentials
	}
	if signer.PublicKey().Type() == ssh.KeyAlgoRSA {
		algorithmSigner, ok := signer.(ssh.AlgorithmSigner)
		if !ok {
			return nil, nil, ErrCredentials
		}
		signer, err = ssh.NewSignerWithAlgorithms(algorithmSigner, hostAlgorithms(signer.PublicKey()))
		if err != nil {
			return nil, nil, ErrCredentials
		}
	}
	return signer, pub, nil
}

// Configure is local administration. The expected public fingerprint must have
// been verified out of band; no network scan or automatic trust is performed.
func (s Store) Configure(ctx context.Context, hostID, keyPath, publicKeyPath, fingerprint string, port int) (Metadata, error) {
	if port < 1 || port > 65535 {
		return Metadata{}, fmt.Errorf("SSH port must be 1–65535")
	}
	var transport string
	if err := s.DB.QueryRowContext(ctx, "SELECT transport FROM hosts WHERE id=?", hostID).Scan(&transport); err != nil {
		return Metadata{}, fmt.Errorf("SSH host profile not found")
	}
	if transport != "ssh" {
		return Metadata{}, fmt.Errorf("profile transport must be ssh")
	}
	private, err := readFile(keyPath, true)
	if err != nil {
		return Metadata{}, fmt.Errorf("private key must be a regular, owner-only file (0600), maximum 64 KiB")
	}
	defer clear(private)
	signer, err := ssh.ParsePrivateKey(private)
	if err != nil || len(hostAlgorithms(signer.PublicKey())) == 0 {
		return Metadata{}, fmt.Errorf("private key is invalid or encrypted; use a dedicated unencrypted key protected by file permissions")
	}
	public, err := readFile(publicKeyPath, false)
	if err != nil {
		return Metadata{}, fmt.Errorf("cannot read host public key file")
	}
	pub, _, _, rest, err := ssh.ParseAuthorizedKey(public)
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		return Metadata{}, fmt.Errorf("host key file must contain one OpenSSH public key")
	}
	if len(hostAlgorithms(pub)) == 0 {
		return Metadata{}, fmt.Errorf("host public key algorithm is not supported securely")
	}
	if fingerprint == "" || ssh.FingerprintSHA256(pub) != fingerprint {
		return Metadata{}, fmt.Errorf("host key fingerprint does not match the explicitly verified fingerprint")
	}
	dir := filepath.Join(s.DataDir, "secrets", "ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Metadata{}, err
	}
	if err := os.Chmod(filepath.Dir(dir), 0700); err != nil {
		return Metadata{}, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return Metadata{}, err
	}
	ref := storage.ID("k_")
	path, err := s.path(ref)
	if err != nil {
		return Metadata{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Metadata{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(path)
		}
	}()
	_, writeErr := f.Write(private)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return Metadata{}, writeErr
	}
	if closeErr != nil {
		return Metadata{}, closeErr
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Metadata{}, err
	}
	defer tx.Rollback()
	oldRef := ""
	err = tx.QueryRowContext(ctx, "SELECT key_ref FROM ssh_credentials WHERE host_id=?", hostID).Scan(&oldRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Metadata{}, err
	}
	meta := Metadata{Configured: true, Port: port, Fingerprint: fingerprint, ConfiguredAt: storage.Now()}
	_, err = tx.ExecContext(ctx, `INSERT INTO ssh_credentials(host_id,credential_id,port,key_ref,host_public_key,host_key_fingerprint,configured_at) VALUES (?,?,?,?,?,?,?) ON CONFLICT(host_id) DO UPDATE SET credential_id=excluded.credential_id,port=excluded.port,key_ref=excluded.key_ref,host_public_key=excluded.host_public_key,host_key_fingerprint=excluded.host_key_fingerprint,configured_at=excluded.configured_at`, hostID, storage.ID("cred_"), port, ref, string(ssh.MarshalAuthorizedKey(pub)), fingerprint, meta.ConfiguredAt)
	if err != nil {
		return Metadata{}, err
	}
	if err = clearObservation(ctx, tx, hostID); err != nil {
		return Metadata{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_events(actor,action,host_id,result,request_id,at) VALUES ('local-admin','ssh.configure',?,'success','local-cli',?)", hostID, storage.Now()); err != nil {
		return Metadata{}, err
	}
	if err = tx.Commit(); err != nil {
		return Metadata{}, err
	}
	committed = true
	if oldRef != "" {
		_ = s.Remove(oldRef)
	}
	return meta, nil
}

func hostAlgorithms(key ssh.PublicKey) []string {
	if _, certificate := key.(*ssh.Certificate); certificate {
		return nil
	}
	algorithms := []string{}
	for _, algorithm := range ssh.SupportedAlgorithms().HostKeys {
		if algorithm == key.Type() || key.Type() == ssh.KeyAlgoRSA && (algorithm == ssh.KeyAlgoRSASHA256 || algorithm == ssh.KeyAlgoRSASHA512) {
			algorithms = append(algorithms, algorithm)
		}
	}
	return algorithms
}

func (s Store) Revoke(ctx context.Context, hostID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ref string
	if err = tx.QueryRowContext(ctx, "SELECT key_ref FROM ssh_credentials WHERE host_id=?", hostID).Scan(&ref); err != nil {
		return ErrRequired
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM ssh_credentials WHERE host_id=?", hostID); err != nil {
		return err
	}
	if err = clearObservation(ctx, tx, hostID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_events(actor,action,host_id,result,request_id,at) VALUES ('local-admin','ssh.revoke',?,'success','local-cli',?)", hostID, storage.Now()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.Remove(ref)
}

func clearObservation(ctx context.Context, tx *sql.Tx, hostID string) error {
	for _, table := range []string{"inventory_cache", "host_observations"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE host_id=?", hostID); err != nil {
			return err
		}
	}
	return nil
}
func (s Store) Remove(ref string) error {
	path, err := s.path(ref)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s Store) path(ref string) (string, error) {
	if !ownedRef.MatchString(ref) {
		return "", ErrCredentials
	}
	return filepath.Join(s.DataDir, "secrets", "ssh", ref), nil
}
func readFile(path string, private bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || (private && info.Mode().Perm()&0077 != 0) {
		return nil, ErrCredentials
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil || len(b) > 64*1024 {
		return nil, ErrCredentials
	}
	return b, nil
}
