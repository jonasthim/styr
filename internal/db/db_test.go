package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if version != 3 {
		t.Fatalf("version = %d, want 3", version)
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
	if version != 3 {
		t.Fatalf("version = %d, want 3", version)
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

// TestMigration_00002_ExistingWorkspacesBecomeSharedPathReady applies only
// 00001_init.sql, seeds a workspace row shaped like the pre-00002 schema,
// then migrates to head and checks the row became a shared ("path",
// unmanaged, ready) workspace with owner_user_id NULL, per the 00002
// migration's stated behaviour for existing rows.
func TestMigration_00002_ExistingWorkspacesBecomeSharedPathReady(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "styr.db")

	sqldb, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = sqldb.Close() }()

	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("SetDialect: %v", err)
	}
	if err := goose.UpToContext(ctx, sqldb, "migrations", 1); err != nil {
		t.Fatalf("migrate to v1: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO workspaces (id, name, path, default_profile_id, worktrees, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"ws-legacy", "legacy", "/srv/legacy", "interactive", 0, now,
	); err != nil {
		t.Fatalf("seed legacy workspace: %v", err)
	}

	if err := goose.UpContext(ctx, sqldb, "migrations"); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}

	row := sqldb.QueryRowContext(ctx,
		`SELECT owner_user_id, source, path, state, managed, worktrees FROM workspaces WHERE id = ?`, "ws-legacy")
	var (
		owner               sql.NullString
		source, gotPath, st string
		managed, wt         int
	)
	if err := row.Scan(&owner, &source, &gotPath, &st, &managed, &wt); err != nil {
		t.Fatalf("scan migrated row: %v", err)
	}
	if owner.Valid {
		t.Errorf("owner_user_id = %v, want NULL", owner)
	}
	if source != "path" {
		t.Errorf("source = %q, want %q", source, "path")
	}
	if gotPath != "/srv/legacy" {
		t.Errorf("path = %q, want %q", gotPath, "/srv/legacy")
	}
	if st != "ready" {
		t.Errorf("state = %q, want %q", st, "ready")
	}
	if managed != 0 {
		t.Errorf("managed = %d, want 0", managed)
	}
	if wt != 0 {
		t.Errorf("worktrees = %d, want 0", wt)
	}

	// The new unique index allows the same name again for a different
	// owner, which the old UNIQUE(name) column constraint would have
	// rejected — confirms the constraint was actually replaced, not just
	// the columns added.
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO users (id, issuer, subject, role, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"u1", "test", "u1", "member", now, now,
	); err != nil {
		t.Fatalf("insert owner user: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO workspaces (id, owner_user_id, name, path, default_profile_id, worktrees, source, repo_url, branch, managed, state, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ws-owned", "u1", "legacy", "/srv/u1/legacy", "interactive", 0, "empty", "", "", 1, "ready", "", now, now,
	); err != nil {
		t.Fatalf("insert owned workspace with same name: %v", err)
	}
}

// TestMigration_00003_AppliesOnTopOfV2WithExistingRows migrates a database
// to version 2, seeds rows in the pre-existing schema (a user, a
// workspace, a session), then migrates to head and checks the new
// triggers-related tables exist, accept inserts that reference the
// existing rows, and enforce their declared constraints.
func TestMigration_00003_AppliesOnTopOfV2WithExistingRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "styr.db")

	sqldb, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = sqldb.Close() }()

	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("SetDialect: %v", err)
	}
	if err := goose.UpToContext(ctx, sqldb, "migrations", 2); err != nil {
		t.Fatalf("migrate to v2: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO users (id, issuer, subject, role, created_at, last_login_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"u1", "test", "u1", "member", now, now,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO workspaces (id, owner_user_id, name, path, default_profile_id, worktrees, source, repo_url, branch, managed, state, error, created_at, updated_at)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ws1", "existing", "/srv/existing", "interactive", 0, "path", "", "", 0, "ready", "", now, now,
	); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO sessions (id, owner_user_id, title, workspace_id, profile_id, harness, state, origin, origin_ref, worktree, created_at, last_active_at)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"s1", "existing session", "ws1", "interactive", "claude", "open", "ui", "", "", now, now,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := goose.UpContext(ctx, sqldb, "migrations"); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}

	version, err := goose.GetDBVersion(sqldb)
	if err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	}
	if version != 3 {
		t.Fatalf("version = %d, want 3", version)
	}

	// Existing rows survive untouched.
	var sessionCount int
	if err := sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = 's1'`).Scan(&sessionCount); err != nil {
		t.Fatalf("count seeded session: %v", err)
	} else if sessionCount != 1 {
		t.Fatalf("expected seeded session to still exist, got count %d", sessionCount)
	}

	// A new table can reference the existing workspace/profile rows.
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO templates (id, owner_user_id, name, workspace_id, profile_id, title_template, prompt_template, system_prompt, report_schema, created_at, updated_at)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"tpl1", "grafana", "ws1", "interactive", "", "investigate {{ .status }}", "", "", now, now,
	); err != nil {
		t.Fatalf("insert template after migration: %v", err)
	}

	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO triggers (id, owner_user_id, name, slug, kind, secret_hash, secret_hint, template_id, enabled,
			dedupe_key_template, cooldown_s, storm_cap_per_hour, run_on_resolved, created_at, updated_at, last_delivery_at)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		"tr1", "grafana alerts", "grafana-alerts", "grafana", "hash", "…ab12", "tpl1", 1, "", 600, 10, 0, now, now,
	); err != nil {
		t.Fatalf("insert trigger after migration: %v", err)
	}

	// A bad kind is rejected by the CHECK constraint added in this
	// migration.
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO triggers (id, owner_user_id, name, slug, kind, secret_hash, secret_hint, template_id, enabled,
			dedupe_key_template, cooldown_s, storm_cap_per_hour, run_on_resolved, created_at, updated_at, last_delivery_at)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		"tr-bad", "bad kind", "bad-kind", "not-a-kind", "hash", "", "tpl1", 1, "", 600, 10, 0, now, now,
	); err == nil {
		t.Fatalf("expected CHECK constraint violation for invalid trigger kind")
	}

	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO runs (id, session_id, template_id, trigger_id, delivery_id, origin, started_at, finished_at, outcome, report, summary, cost_usd)
		 VALUES (?, ?, ?, NULL, NULL, ?, ?, NULL, ?, '', '', 0)`,
		"run1", "s1", "tpl1", "webhook", now, "running",
	); err != nil {
		t.Fatalf("insert run referencing existing session: %v", err)
	}

	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, created_at, last_used_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)`,
		"tok1", "u1", "ci", "hash123", "styr_pat_", now,
	); err != nil {
		t.Fatalf("insert api token referencing existing user: %v", err)
	}
}
