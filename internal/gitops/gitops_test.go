package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv returns a whitelisted environment for git commands in tests: a
// throwaway HOME (so no global .gitconfig leaks in), a real PATH (so the
// system git binary is found), GIT_TERMINAL_PROMPT=0, and a fixed author
// so commits made directly by the test helpers (not via -c flags) are
// deterministic.
func testEnv(t *testing.T) []string {
	t.Helper()
	return []string{
		"HOME=" + t.TempDir(),
		"PATH=" + os.Getenv("PATH"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	}
}

// runInDir runs a git subcommand directly (bypassing this package, for
// test setup) and fails the test on error.
func runInDir(t *testing.T, dir string, env []string, args ...string) string {
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo creates a fresh git repo with one commit (README.md) and returns
// its path, that commit's sha, and a whitelisted env to use with it.
func newRepo(t *testing.T) (dir, baseSHA string, env []string) {
	t.Helper()
	dir = t.TempDir()
	env = testEnv(t)
	runInDir(t, dir, env, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "README.md"), "hello\n")
	runInDir(t, dir, env, "add", "-A")
	runInDir(t, dir, env, "commit", "-q", "-m", "init")
	baseSHA = strings.TrimSpace(runInDir(t, dir, env, "rev-parse", "HEAD"))
	return dir, baseSHA, env
}

func TestDefaultEnvStripsGitVarsExceptTerminalPrompt(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "should-be-stripped")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	env := defaultEnv()
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") && kv != "GIT_TERMINAL_PROMPT=0" {
			t.Fatalf("unexpected GIT_ var leaked into default env: %q", kv)
		}
	}
	found := false
	for _, kv := range env {
		if kv == "GIT_TERMINAL_PROMPT=0" {
			found = true
		}
	}
	if !found {
		t.Fatal("GIT_TERMINAL_PROMPT=0 missing from default env")
	}
}
