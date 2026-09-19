package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
)

// writeFakeClaude writes an executable stub named "claude" into t.TempDir()
// that answers --version with exit 0, and returns its path.
func writeFakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/usr/bin/env bash\necho 'fake-claude 2.1.276'\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func baseTestConfig(t *testing.T, claudeBin string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Env = "dev"
	cfg.DataDir = t.TempDir()
	cfg.SecretKey = "this-is-a-32-plus-byte-secret-key!!"
	cfg.ClaudeBin = claudeBin
	return cfg
}

func findCheck(t *testing.T, checks []check, name string) check {
	t.Helper()
	for _, c := range checks {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no check named %q", name)
	return check{}
}

func TestDoctorChecks_ClaudeBinaryFound(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "claude binary found and --version runs")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_ClaudeBinaryMissing(t *testing.T) {
	cfg := baseTestConfig(t, filepath.Join(t.TempDir(), "does-not-exist"))
	c := findCheck(t, doctorChecks(cfg), "claude binary found and --version runs")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for missing binary")
	}
}

func TestDoctorChecks_CodexBinaryFound(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.CodexBin = writeFakeClaude(t) // any executable answering --version will do
	c := findCheck(t, doctorChecks(cfg), "codex binary found (optional)")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_CodexBinaryMissingIsSkip(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.CodexBin = filepath.Join(t.TempDir(), "does-not-exist")
	c := findCheck(t, doctorChecks(cfg), "codex binary found (optional)")
	err := c.run()
	if !errors.Is(err, errSkip) {
		t.Fatalf("run() = %v, want errSkip: Codex is optional", err)
	}
}

func TestDoctorChecks_DataDirWritable(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "data dir writable")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_DataDirNotWritable(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.DataDir = filepath.Join(cfg.DataDir, "nested", "deep")
	// Make the parent read-only so MkdirAll on the nested dir fails.
	parent := filepath.Dir(filepath.Dir(cfg.DataDir))
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks do not apply")
	}
	c := findCheck(t, doctorChecks(cfg), "data dir writable")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for unwritable data dir")
	}
}

func TestDoctorChecks_GitOnPath(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "git on PATH")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil (is git installed in this environment?)", err)
	}
}

func TestDoctorChecks_SecretKeyLength(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.SecretKey = "too-short"
	c := findCheck(t, doctorChecks(cfg), "secret key length")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for short secret key")
	}

	cfg.SecretKey = "this-is-a-32-plus-byte-secret-key!!"
	c = findCheck(t, doctorChecks(cfg), "secret key length")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_DatabaseOpens(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "database opens")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_OIDCSkippedInDev(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.Env = "dev"
	c := findCheck(t, doctorChecks(cfg), "oidc discovery reachable")
	err := c.run()
	if err == nil {
		t.Fatal("run() = nil, want a skip sentinel")
	}
	if !errors.Is(err, errSkip) {
		t.Fatalf("run() = %v, want errSkip", err)
	}
}

func TestDoctorChecks_OIDCReachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"issuer":"` + r.Host + `"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.Env = "prod"
	cfg.BaseURL = "https://styr.example.com"
	cfg.OIDC = []config.OIDCProvider{{Name: "Test", Issuer: ts.URL, ClientID: "abc"}}

	c := findCheck(t, doctorChecks(cfg), "oidc discovery reachable")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_OIDCUnreachable(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.Env = "prod"
	cfg.BaseURL = "https://styr.example.com"
	cfg.OIDC = []config.OIDCProvider{{Name: "Test", Issuer: "http://127.0.0.1:1", ClientID: "abc"}}

	c := findCheck(t, doctorChecks(cfg), "oidc discovery reachable")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for unreachable issuer")
	}
}

func TestRunDoctor_ExitsOneOnFailure(t *testing.T) {
	t.Setenv("STYR_CONFIG", "")
	t.Setenv("STYR_ENV", "dev")
	t.Setenv("STYR_DATA_DIR", t.TempDir())
	t.Setenv("STYR_SECRET_KEY", "too-short")
	t.Setenv("STYR_CLAUDE_BIN", writeFakeClaude(t))

	var out fakeWriter
	if code := runDoctor(&out, nil); code != 1 {
		t.Fatalf("runDoctor() = %d, want 1; output:\n%s", code, out.String())
	}
	if !containsLine(out.String(), "FAIL secret key length") {
		t.Fatalf("output missing FAIL line:\n%s", out.String())
	}
}

func TestRunDoctor_ExitsZeroWhenHealthy(t *testing.T) {
	t.Setenv("STYR_CONFIG", "")
	t.Setenv("STYR_ENV", "dev")
	t.Setenv("STYR_DATA_DIR", t.TempDir())
	t.Setenv("STYR_SECRET_KEY", "this-is-a-32-plus-byte-secret-key!!")
	t.Setenv("STYR_CLAUDE_BIN", writeFakeClaude(t))

	var out fakeWriter
	if code := runDoctor(&out, nil); code != 0 {
		t.Fatalf("runDoctor() = %d, want 0; output:\n%s", code, out.String())
	}
	if strings.Contains(out.String(), ": skip: ") || strings.Contains(out.String(), ": warn: ") {
		t.Fatalf("status prefix doubled in doctor output:\n%s", out.String())
	}
	if !containsLine(out.String(), "skip oidc discovery reachable") {
		t.Fatalf("output missing skip line:\n%s", out.String())
	}
}

// fakeWriter is a minimal io.Writer collecting output for assertions,
// avoiding a bytes.Buffer import just for this.
type fakeWriter struct{ buf []byte }

func (w *fakeWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}
func (w *fakeWriter) String() string { return string(w.buf) }

func containsLine(s, prefix string) bool {
	for _, line := range splitLines(s) {
		if len(line) >= len(prefix) && line[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func TestLoadEnvFileSetsOnlyUnsetKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env")
	if err := os.WriteFile(path, []byte("# comment\nSTYR_TEST_A=one\nexport STYR_TEST_B=\"two words\"\nSTYR_TEST_C='three'\nbroken line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STYR_TEST_A", "preset")
	os.Unsetenv("STYR_TEST_B")
	os.Unsetenv("STYR_TEST_C")
	t.Cleanup(func() { os.Unsetenv("STYR_TEST_B"); os.Unsetenv("STYR_TEST_C") })
	if n := loadEnvFile(path); n != 2 {
		t.Fatalf("set %d keys, want 2", n)
	}
	if os.Getenv("STYR_TEST_A") != "preset" || os.Getenv("STYR_TEST_B") != "two words" || os.Getenv("STYR_TEST_C") != "three" {
		t.Fatalf("env = %q %q %q", os.Getenv("STYR_TEST_A"), os.Getenv("STYR_TEST_B"), os.Getenv("STYR_TEST_C"))
	}
	if loadEnvFile(filepath.Join(dir, "missing")) != 0 {
		t.Fatal("missing file must set nothing")
	}
}

// gitTestEnv returns a whitelisted environment for driving git directly in
// tests: a throwaway HOME (no global .gitconfig leaks in), a real PATH, and
// a fixed author identity.
func gitTestEnv(t *testing.T) []string {
	t.Helper()
	return []string{
		"HOME=" + t.TempDir(),
		"PATH=" + os.Getenv("PATH"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	}
}

// runGitInDir runs a git subcommand directly (bypassing internal/gitops,
// for test setup) and fails the test on error.
func runGitInDir(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// newGitWorkspace creates a fresh git repo at t.TempDir() with one commit,
// registers it as a workspace row in d, and returns its path.
func newGitWorkspace(t *testing.T, ctx context.Context, d *db.DB, name string) (wsPath string) {
	t.Helper()
	wsPath = t.TempDir()
	env := gitTestEnv(t)
	runGitInDir(t, wsPath, env, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(wsPath, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitInDir(t, wsPath, env, "add", "-A")
	runGitInDir(t, wsPath, env, "commit", "-q", "-m", "init")

	now := time.Now()
	ws := domain.Workspace{
		ID:               uuid.NewString(),
		Name:             name,
		Path:             wsPath,
		DefaultProfileID: "interactive",
		Source:           domain.WorkspaceSourcePath,
		State:            domain.WorkspaceReady,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := db.NewWorkspaces(d).Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return wsPath
}

// addOrphanWorktree creates a real git worktree under wsPath's
// .styr/worktrees directory (on its own styr/<name> branch) that no
// session row will ever reference, and returns its path.
func addOrphanWorktree(t *testing.T, wsPath, name string) string {
	t.Helper()
	env := gitTestEnv(t)
	wtDir := filepath.Join(wsPath, ".styr", "worktrees", name)
	runGitInDir(t, wsPath, env, "worktree", "add", "-b", "styr/"+name, wtDir, "HEAD")
	return wtDir
}

func TestDoctorChecks_OrphanWorktrees_DetectAndPrune(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	d, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	ctx := context.Background()
	wsPath := newGitWorkspace(t, ctx, d, "ws1")
	orphanPath := addOrphanWorktree(t, wsPath, "orphan1")
	if err := d.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	c := findCheck(t, doctorChecks(cfg), "orphan session worktrees")
	err = c.run()
	if err == nil {
		t.Fatal("run() = nil, want a warning for the orphan worktree")
	}
	if !errors.Is(err, errWarn) {
		t.Fatalf("run() = %v, want errWarn", err)
	}
	if !strings.Contains(err.Error(), "1 orphan worktree") {
		t.Fatalf("run() = %v, want it to mention 1 orphan worktree", err)
	}

	removed, _, err := pruneOrphanWorktrees(ctx, cfg)
	if err != nil {
		t.Fatalf("pruneOrphanWorktrees: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, statErr := os.Stat(orphanPath); !os.IsNotExist(statErr) {
		t.Fatalf("orphan worktree %s still exists after pruning: %v", orphanPath, statErr)
	}

	c = findCheck(t, doctorChecks(cfg), "orphan session worktrees")
	if err := c.run(); err != nil {
		t.Fatalf("run() after prune = %v, want nil", err)
	}
}

func TestDoctorChecks_OrphanWorktrees_NoWorkspacesIsOK(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "orphan session worktrees")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil with no registered workspaces", err)
	}
}

func TestDoctorChecks_PIDFileLiveness(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "pid file liveness")
	if err := c.run(); err != nil {
		t.Fatalf("run() with no pid file = %v, want nil", err)
	}

	if err := writePIDFile(pidFilePath(cfg.DataDir)); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	c = findCheck(t, doctorChecks(cfg), "pid file liveness")
	if err := c.run(); err != nil {
		t.Fatalf("run() with this process's own live pid = %v, want nil", err)
	}

	// A short-lived child process's pid is dead by the time Run() returns
	// (it has exited and been reaped), so writing it to the pid file
	// simulates a stale file left behind by a server that crashed instead
	// of shutting down cleanly.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run `true`: %v", err)
	}
	deadPID := cmd.Process.Pid
	if err := os.WriteFile(pidFilePath(cfg.DataDir), []byte(strconv.Itoa(deadPID)), 0o644); err != nil {
		t.Fatal(err)
	}
	c = findCheck(t, doctorChecks(cfg), "pid file liveness")
	err := c.run()
	if err == nil {
		t.Fatal("run() with a stale pid file = nil, want a warning")
	}
	if !errors.Is(err, errWarn) {
		t.Fatalf("run() = %v, want errWarn", err)
	}
}

func TestRunDoctor_PruneWorktreesFlag(t *testing.T) {
	t.Setenv("STYR_CONFIG", "")
	t.Setenv("STYR_ENV", "dev")
	dataDir := t.TempDir()
	t.Setenv("STYR_DATA_DIR", dataDir)
	t.Setenv("STYR_SECRET_KEY", "this-is-a-32-plus-byte-secret-key!!")
	t.Setenv("STYR_CLAUDE_BIN", writeFakeClaude(t))

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.DataDir = dataDir
	d, err := db.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	ctx := context.Background()
	wsPath := newGitWorkspace(t, ctx, d, "ws1")
	addOrphanWorktree(t, wsPath, "orphan1")
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	var out fakeWriter
	if code := runDoctor(&out, []string{"--prune-worktrees"}); code != 0 {
		t.Fatalf("runDoctor(--prune-worktrees) = %d, want 0; output:\n%s", code, out.String())
	}
	if !containsLine(out.String(), "note pruned 1 orphan worktree") {
		t.Fatalf("output missing prune confirmation:\n%s", out.String())
	}
}
