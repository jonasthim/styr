// Package db opens the SQLite database and applies embedded goose
// migrations. Every repository (Users, Sessions, Events, ...) wraps a *DB.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB wraps a *sql.DB opened against Styr's SQLite database.
type DB struct{ *sql.DB }

// Open opens (creating if needed) the database at path with WAL, foreign
// keys and a 5s busy timeout, then migrates it to the latest version. The
// file (and its WAL/SHM siblings) end up owner-only since the database holds
// encrypted Claude tokens and session cookies.
func Open(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite handles one writer; keep a small pool to avoid lock churn.
	sqldb.SetMaxOpenConns(4)
	ctx := context.Background()
	if err := sqldb.PingContext(ctx); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(ctx, sqldb); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0o600); err != nil && !os.IsNotExist(err) {
			_ = sqldb.Close()
			return nil, fmt.Errorf("restrict permissions on %s: %w", path+suffix, err)
		}
	}
	return &DB{sqldb}, nil
}

// migrate applies all pending migrations.
func migrate(ctx context.Context, sqldb *sql.DB) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqldb, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
