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
	if version != 8 {
		t.Fatalf("version = %d, want 8", version)
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
	if version != 8 {
		t.Fatalf("version = %d, want 8", version)
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

// TestMigration_00008_PipelinesTablesAndColumns migrates a fresh database
// to head and checks that the pipelines/pipeline_runs/step_runs tables
// exist with the shape 00008_pipelines.sql declares, and that triggers,
// schedules and runs gained their new nullable pipeline_id/pipeline_id/
// step_run_id columns.
func TestMigration_00008_PipelinesTablesAndColumns(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)

	seedTemplateFixtures(t, database)
	now := time.Now()

	// pipelines: create, unique index on (owner_user_id, name).
	if _, err := database.ExecContext(ctx,
		`INSERT INTO pipelines (id, name, workspace_id, yaml, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"pl1", "fix-ci", "ws1", "name: fix-ci\n", nowString(now), nowString(now),
	); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO pipelines (id, name, workspace_id, yaml, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"pl2", "fix-ci", "ws1", "name: fix-ci\n", nowString(now), nowString(now),
	); err == nil {
		t.Fatal("insert duplicate shared pipeline name: want unique index violation")
	}

	// pipeline_runs: state CHECK constraint and the pipeline_id foreign key.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO pipeline_runs (id, pipeline_id, origin, state, started_at) VALUES (?, ?, ?, ?, ?)`,
		"pr1", "pl1", "ui", "running", nowString(now),
	); err != nil {
		t.Fatalf("insert pipeline run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO pipeline_runs (id, pipeline_id, origin, state, started_at) VALUES (?, ?, ?, ?, ?)`,
		"pr-bad-state", "pl1", "ui", "bogus", nowString(now),
	); err == nil {
		t.Fatal("insert pipeline run with invalid state: want CHECK violation")
	}

	// step_runs: state CHECK constraint, pipeline_run_id foreign key, and
	// the step_runs_pipeline_idx index (queried below).
	if _, err := database.ExecContext(ctx,
		`INSERT INTO step_runs (id, pipeline_run_id, step_id, state) VALUES (?, ?, ?, ?)`,
		"sr1", "pr1", "triage", "pending",
	); err != nil {
		t.Fatalf("insert step run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO step_runs (id, pipeline_run_id, step_id, state) VALUES (?, ?, ?, ?)`,
		"sr-bad-state", "pr1", "triage", "bogus",
	); err == nil {
		t.Fatal("insert step run with invalid state: want CHECK violation")
	}
	row := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM step_runs WHERE pipeline_run_id = ?`, "pr1")
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count step runs: %v", err)
	}
	if n != 1 {
		t.Fatalf("step run count = %d, want 1", n)
	}

	// triggers/schedules gained a nullable pipeline_id column; runs gained
	// a nullable step_run_id column. WHERE 1 = 0 matches no rows, so this
	// only needs the columns to exist, not any FK-satisfying seed data.
	if _, err := database.ExecContext(ctx,
		`UPDATE triggers SET pipeline_id = ? WHERE 1 = 0`, "pl1"); err != nil {
		t.Fatalf("triggers.pipeline_id does not exist: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE schedules SET pipeline_id = ? WHERE 1 = 0`, "pl1"); err != nil {
		t.Fatalf("schedules.pipeline_id does not exist: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET step_run_id = ? WHERE 1 = 0`, "sr1"); err != nil {
		t.Fatalf("runs.step_run_id does not exist: %v", err)
	}
}
