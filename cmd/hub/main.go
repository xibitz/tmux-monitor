package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xibitz/tmux-monitor/internal/config"
	sshconnector "github.com/xibitz/tmux-monitor/internal/connector/ssh"
	"github.com/xibitz/tmux-monitor/internal/httpapi"
	"github.com/xibitz/tmux-monitor/internal/identity"
	"github.com/xibitz/tmux-monitor/internal/storage"
	"github.com/xibitz/tmux-monitor/internal/webassets"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hub:", err)
		os.Exit(1)
	}
}

func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "serve" && command != "bootstrap" && command != "migrate" && command != "ssh-configure" && command != "ssh-revoke" {
		return fmt.Errorf("usage: hub [serve|bootstrap|migrate|ssh-configure|ssh-revoke]")
	}
	// Bootstrap and migrations are local-only and do not need a public origin.
	dataDir := os.Getenv("TMUX_MONITOR_DATA_DIR")
	if dataDir == "" {
		dataDir = ".data"
	}
	var cfg config.Config
	var err error
	if command == "serve" {
		if len(os.Args) > 2 {
			return fmt.Errorf("serve takes no arguments; use environment configuration")
		}
		cfg, err = config.Load()
		if err != nil {
			return err
		}
		dataDir = cfg.DataDir
	}
	db, err := storage.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open data directory: %w", err)
	}
	defer db.Close()
	ctx := context.Background()
	if command == "migrate" {
		flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
		target := flags.Int("to", storage.LatestVersion, "schema version (0 removes foundation tables)")
		confirm := flags.Bool("allow-data-loss", false, "allow explicit rollback with data loss")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected migration arguments")
		}
		if *target < storage.LatestVersion && !*confirm {
			return fmt.Errorf("rollback removes data; back up SQLite first, then pass --allow-data-loss")
		}
		if err := storage.Migrate(ctx, db, *target); err != nil {
			return err
		}
		fmt.Println("Schema version:", strconv.Itoa(*target))
		return nil
	}
	if err := storage.Migrate(ctx, db, storage.LatestVersion); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if command == "ssh-configure" || command == "ssh-revoke" {
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		hostID := flags.String("host-id", "", "SSH host profile ID")
		key := flags.String("key-file", "", "dedicated private key file (owner-only permissions)")
		public := flags.String("host-key-file", "", "one public host key verified out of band")
		fingerprint := flags.String("expected-fingerprint", "", "verified SHA256 host key fingerprint")
		port := flags.Int("port", 22, "SSH port")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *hostID == "" || flags.NArg() != 0 {
			return fmt.Errorf("--host-id is required; no positional arguments allowed")
		}
		store := sshconnector.Store{DB: db, DataDir: dataDir}
		if command == "ssh-revoke" {
			if err := store.Revoke(ctx, *hostID); err != nil {
				return err
			}
			fmt.Println("SSH configuration revoked.")
			return nil
		}
		if *key == "" || *public == "" || *fingerprint == "" {
			return fmt.Errorf("--key-file, --host-key-file and --expected-fingerprint are required")
		}
		meta, err := store.Configure(ctx, *hostID, *key, *public, *fingerprint, *port)
		if err != nil {
			return err
		}
		fmt.Println("SSH configuration saved. Verified host key:", meta.Fingerprint)
		return nil
	}
	if command == "bootstrap" {
		flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
		username := flags.String("username", "admin", "initial administrator username")
		stdin := flags.Bool("password-stdin", false, "read password from stdin instead of terminal prompt")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected bootstrap arguments")
		}
		var password []byte
		if *stdin {
			reader := bufio.NewReaderSize(os.Stdin, 2048)
			password, err = reader.ReadSlice('\n')
			if err != nil {
				return fmt.Errorf("stdin password must be one line ending with a newline")
			}
			password = []byte(strings.TrimSuffix(strings.TrimSuffix(string(password), "\n"), "\r"))
		} else {
			fmt.Fprint(os.Stderr, "Admin password (minimum 12 characters): ")
			password, err = term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fmt.Errorf("cannot read password; use an interactive terminal or --password-stdin")
			}
			fmt.Fprint(os.Stderr, "Repeat password: ")
			confirmation, e := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if e != nil || string(password) != string(confirmation) {
				return fmt.Errorf("passwords do not match")
			}
		}
		if err := identity.Bootstrap(ctx, db, *username, string(password)); err != nil {
			return err
		}
		clear(password)
		fmt.Println("Initial administrator created.")
		return nil
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := &http.Server{Addr: cfg.Listen, Handler: httpapi.New(db, cfg, webassets.Files(), logger), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	listener, err := net.Listen("tcp4", cfg.Listen)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.Info("hub started", "listen", cfg.Listen, "origin", cfg.Origin, "development", cfg.Development)
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
	}
	return nil
}
