package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentBranch(t *testing.T) {
	dir, _, env := newRepo(t)
	repo := Repo{Path: dir, Env: env}
	branch, err := repo.CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}
}

func TestAddWorktreeAndExcludeIdempotent(t *testing.T) {
	dir, base, env := newRepo(t)
	repo := Repo{Path: dir, Env: env}
	ctx := context.Background()

	wtDir := filepath.Join(dir, ".styr", "worktrees", "s1")
	if err := repo.AddWorktree(ctx, wtDir, "styr/s1-fix", base); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtDir, "README.md")); err != nil {
		t.Fatalf("worktree missing files: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if strings.Count(string(exclude), ".styr/") != 1 {
		t.Fatalf("exclude file = %q, want exactly one .styr/ entry", exclude)
	}

	// Adding a second worktree must not duplicate the exclude entry.
	wtDir2 := filepath.Join(dir, ".styr", "worktrees", "s2")
	if err := repo.AddWorktree(ctx, wtDir2, "styr/s2-fix", base); err != nil {
		t.Fatalf("AddWorktree #2: %v", err)
	}
	exclude, err = os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if strings.Count(string(exclude), ".styr/") != 1 {
		t.Fatalf("exclude file after 2nd add = %q, want still exactly one .styr/ entry", exclude)
	}
}

func TestAddWorktreeRejectsBadBranch(t *testing.T) {
	dir, base, env := newRepo(t)
	repo := Repo{Path: dir, Env: env}
	err := repo.AddWorktree(context.Background(), filepath.Join(dir, "wt"), "not-a-styr-branch", base)
	if !errors.Is(err, ErrInvalidBranch) {
		t.Fatalf("err = %v, want ErrInvalidBranch", err)
	}
}

func TestRemoveWorktree(t *testing.T) {
	dir, base, env := newRepo(t)
	repo := Repo{Path: dir, Env: env}
	ctx := context.Background()
	wtDir := filepath.Join(dir, ".styr", "worktrees", "s1")
	if err := repo.AddWorktree(ctx, wtDir, "styr/s1-fix", base); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := repo.RemoveWorktree(ctx, wtDir, true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still exists")
	}
	out := runInDir(t, dir, env, "branch", "--list", "styr/s1-fix")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("branch still exists: %q", out)
	}
}

func TestWorktreeInfo(t *testing.T) {
	dir, base, env := newRepo(t)
	repo := Repo{Path: dir, Env: env}
	ctx := context.Background()
	wtDir := filepath.Join(dir, ".styr", "worktrees", "s1")
	if err := repo.AddWorktree(ctx, wtDir, "styr/s1-fix", base); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	info, err := repo.WorktreeInfo(ctx, wtDir)
	if err != nil {
		t.Fatalf("WorktreeInfo: %v", err)
	}
	if info.Path != wtDir {
		t.Errorf("Path = %q, want %q", info.Path, wtDir)
	}
	if info.Branch != "styr/s1-fix" {
		t.Errorf("Branch = %q, want styr/s1-fix", info.Branch)
	}
	if info.BaseRef != "" {
		t.Errorf("BaseRef = %q, want empty (not recoverable from git alone)", info.BaseRef)
	}
}

func TestWorktreeInfoNotRegisteredIsErrNotWorktree(t *testing.T) {
	dir, _, env := newRepo(t)
	repo := Repo{Path: dir, Env: env}
	if _, err := repo.WorktreeInfo(context.Background(), filepath.Join(dir, ".styr", "worktrees", "nope")); !errors.Is(err, ErrNotWorktree) {
		t.Fatalf("WorktreeInfo(unregistered) = %v, want ErrNotWorktree", err)
	}
}

func TestRemoveWorktreeMissingDirIsNotError(t *testing.T) {
	dir, base, env := newRepo(t)
	repo := Repo{Path: dir, Env: env}
	ctx := context.Background()
	wtDir := filepath.Join(dir, ".styr", "worktrees", "s1")
	if err := repo.AddWorktree(ctx, wtDir, "styr/s1-fix", base); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := os.RemoveAll(wtDir); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveWorktree(ctx, wtDir, false); err != nil {
		t.Fatalf("RemoveWorktree on missing dir: %v", err)
	}
}
