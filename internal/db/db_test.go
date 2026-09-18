package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// testOpenDB opens a fresh migrated database in a per-test temp directory,
// closing it automatically on cleanup. Shared by every repository test in
// this package.
func testOpenDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "styr.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestOpen_CreatesFileAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "styr.db")

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected db file to exist: %v", err)
	}

	version, err := goose.GetDBVersion(d.DB)
	if err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
}

func TestOpen_IdempotentReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "styr.db")

	d1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	d2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer d2.Close()

	version, err := goose.GetDBVersion(d2.DB)
	if err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
}

func TestOpen_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "styr.db")

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}
}
