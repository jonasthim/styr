// Package gitops drives the git and gh command-line binaries to manage
// per-session worktrees, diffs, checkpoints, commits, pushes and pull
// requests for a Styr workspace.
//
// It invokes only the git and gh binaries directly (standard library
// os/exec, argv-based, no shell), under a whitelisted environment (see
// Repo.Env and Worktree.Env) and a 60 second timeout per command.
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// cmdTimeout bounds every git/gh invocation.
const cmdTimeout = 60 * time.Second

// maxDiffBytes caps the size of a single file's diff (tracked or
// synthesized for an untracked file) before it is truncated.
const maxDiffBytes = 2 * 1024 * 1024

// Sentinel errors returned by this package. Callers should use errors.Is.
var (
	ErrNothingToCommit = errors.New("gitops: nothing to commit")
	ErrNoRemote        = errors.New("gitops: no remote configured")
	ErrGHUnavailable   = errors.New("gitops: gh is not available or not authenticated")
	ErrNotAncestor     = errors.New("gitops: commit is not an ancestor of HEAD")
	ErrInvalidBranch   = errors.New("gitops: invalid branch name")
)

// Repo is a workspace's main git checkout: the one AddWorktree and
// RemoveWorktree operate on.
type Repo struct {
	Path string
	// Env is the whitelisted environment passed to git; nil uses
	// defaultEnv(), the process environment with every GIT_-prefixed
	// variable removed except GIT_TERMINAL_PROMPT=0, which is always
	// present.
	Env []string
}

// Worktree is one session's git worktree, checked out on its own branch
// from BaseRef.
type Worktree struct {
	Path    string
	Branch  string
	BaseRef string
	// Env is the whitelisted environment passed to git/gh; nil uses
	// defaultEnv() (see Repo.Env).
	Env []string
}

// Author names the identity a Commit is made under.
type Author struct {
	Name  string
	Email string
}

// Commit is one entry from Worktree.Log.
type Commit struct {
	SHA     string
	Subject string
	Date    time.Time
}

func (r Repo) env() []string {
	if r.Env != nil {
		return r.Env
	}
	return defaultEnv()
}

func (w Worktree) env() []string {
	if w.Env != nil {
		return w.Env
	}
	return defaultEnv()
}

// defaultEnv is the process environment with every GIT_-prefixed variable
// removed, plus GIT_TERMINAL_PROMPT=0 so git never blocks on a prompt this
// package cannot answer.
func defaultEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GIT_TERMINAL_PROMPT=0")
}

// envValue returns the value of key in env, or "" when absent.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

// lookPath resolves name to an executable path using env's own PATH,
// never the current process's, so Repo.Env/Worktree.Env (and tests) fully
// control which binaries are found.
func lookPath(env []string, name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		if isExecutable(name) {
			return name, nil
		}
		return "", fmt.Errorf("gitops: %s: not found", name)
	}
	for _, dir := range filepath.SplitList(envValue(env, "PATH")) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("gitops: %s: not found in PATH", name)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// runCmd runs name with args in dir under env: no shell, resolved via
// env's own PATH, bounded by cmdTimeout. It returns stdout, stderr (as
// text, for error messages) and the run error (an *exec.ExitError for a
// nonzero exit).
func runCmd(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, string, error) {
	resolved, err := lookPath(env, name)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, resolved, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.String(), err
}

// runGit runs a git subcommand in dir under env.
func runGit(ctx context.Context, dir string, env []string, args ...string) ([]byte, string, error) {
	return runCmd(ctx, dir, env, "git", args...)
}

// isCleanExit reports whether err is the "no differences" exit status 1
// from a `git diff --quiet`-style command, as opposed to a real failure
// (a different exit code, or the command never ran at all).
func isCleanExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lastNonEmptyLine returns the last non-blank line of s.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
