package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// branchRe matches an allowed Styr session branch name.
var branchRe = regexp.MustCompile(`^styr/[a-z0-9-]{1,60}$`)

// CurrentBranch returns the repo's currently checked-out branch (e.g. for
// use as AddWorktree's base when the caller wants "whatever HEAD is now").
func (r Repo) CurrentBranch(ctx context.Context) (string, error) {
	out, stderr, err := runGit(ctx, r.Path, r.env(), "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitops: current branch: %w: %s", err, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// AddWorktree creates a new worktree at dir on a new branch (git worktree
// add -b branch dir base), validating the branch name against
// ErrInvalidBranch, and ensures the repo's own status ignores the
// worktrees root via .git/info/exclude (idempotent: the entry is added at
// most once). base "" uses the repo's current HEAD.
func (r Repo) AddWorktree(ctx context.Context, dir, branch, base string) error {
	if !branchRe.MatchString(branch) {
		return fmt.Errorf("%w: %q", ErrInvalidBranch, branch)
	}
	if base == "" {
		base = "HEAD"
	}
	env := r.env()
	if out, stderr, err := runGit(ctx, r.Path, env, "worktree", "add", "-b", branch, dir, base); err != nil {
		return fmt.Errorf("gitops: add worktree: %w: %s%s", err, out, stderr)
	}
	return r.excludeStyrDir()
}

// excludeStyrDir appends ".styr/" to the repo's .git/info/exclude, once.
func (r Repo) excludeStyrDir() error {
	const entry = ".styr/"
	excludePath := filepath.Join(r.Path, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("gitops: create git info dir: %w", err)
	}
	data, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("gitops: read exclude file: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("gitops: open exclude file: %w", err)
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	if _, err := f.WriteString(prefix + entry + "\n"); err != nil {
		return fmt.Errorf("gitops: write exclude file: %w", err)
	}
	return nil
}

// RemoveWorktree removes the worktree at dir (a missing dir is not an
// error), optionally deletes its branch, then prunes stale worktree
// administrative files.
func (r Repo) RemoveWorktree(ctx context.Context, dir string, deleteBranch bool) error {
	env := r.env()
	branch, _ := r.worktreeBranch(ctx, env, dir)

	if fileExists(dir) {
		if out, stderr, err := runGit(ctx, r.Path, env, "worktree", "remove", "--force", dir); err != nil {
			return fmt.Errorf("gitops: remove worktree: %w: %s%s", err, out, stderr)
		}
	}
	if deleteBranch && branch != "" {
		if out, stderr, err := runGit(ctx, r.Path, env, "branch", "-D", branch); err != nil {
			return fmt.Errorf("gitops: delete branch %s: %w: %s%s", branch, err, out, stderr)
		}
	}
	_, _, _ = runGit(ctx, r.Path, env, "worktree", "prune")
	return nil
}

// worktreeEntry is one entry of `git worktree list --porcelain`.
type worktreeEntry struct {
	Path   string
	Branch string
}

// listWorktrees parses `git worktree list --porcelain`.
func (r Repo) listWorktrees(ctx context.Context, env []string) ([]worktreeEntry, error) {
	out, stderr, err := runGit(ctx, r.Path, env, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("gitops: worktree list: %w: %s", err, stderr)
	}
	var entries []worktreeEntry
	var cur worktreeEntry
	flush := func() {
		if cur.Path != "" {
			entries = append(entries, cur)
		}
		cur = worktreeEntry{}
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "":
			flush()
		}
	}
	flush()
	return entries, nil
}

// worktreeBranch returns the branch checked out at dir, if any worktree is
// currently registered there ("" otherwise).
func (r Repo) worktreeBranch(ctx context.Context, env []string, dir string) (string, error) {
	entries, err := r.listWorktrees(ctx, env)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	for _, e := range entries {
		absE, err := filepath.Abs(e.Path)
		if err != nil {
			absE = e.Path
		}
		if absE == absDir {
			return e.Branch, nil
		}
	}
	return "", nil
}
