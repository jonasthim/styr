package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointDirtyAndClean(t *testing.T) {
	dir, base, env := newRepo(t)
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	ctx := context.Background()

	// Clean: nothing changed since init commit.
	sha, dirty, err := w.Checkpoint(ctx, "styr: checkpoint 1")
	if err != nil {
		t.Fatalf("Checkpoint (clean): %v", err)
	}
	if dirty {
		t.Fatalf("dirty = true, want false on a clean worktree")
	}
	if sha != base {
		t.Fatalf("sha = %s, want base %s", sha, base)
	}

	// Dirty: change a file.
	writeFile(t, filepath.Join(dir, "README.md"), "hello again\n")
	sha2, dirty2, err := w.Checkpoint(ctx, "styr: checkpoint 2")
	if err != nil {
		t.Fatalf("Checkpoint (dirty): %v", err)
	}
	if !dirty2 {
		t.Fatalf("dirty = false, want true")
	}
	if sha2 == base {
		t.Fatalf("sha did not change after a dirty checkpoint")
	}

	// Clean again immediately after.
	_, dirty3, err := w.Checkpoint(ctx, "styr: checkpoint 3")
	if err != nil {
		t.Fatalf("Checkpoint (clean again): %v", err)
	}
	if dirty3 {
		t.Fatalf("dirty = true, want false right after a checkpoint")
	}
}

func TestLog(t *testing.T) {
	dir, base, env := newRepo(t)
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	ctx := context.Background()

	writeFile(t, filepath.Join(dir, "a.txt"), "1\n")
	sha1, _, err := w.Checkpoint(ctx, "styr: checkpoint a")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "2\n")
	sha2, _, err := w.Checkpoint(ctx, "styr: checkpoint b")
	if err != nil {
		t.Fatal(err)
	}

	commits, err := w.Log(ctx, "")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2: %+v", len(commits), commits)
	}
	if commits[0].SHA != sha2 || commits[1].SHA != sha1 {
		t.Fatalf("commits = %+v, want newest first [%s, %s]", commits, sha2, sha1)
	}
	for _, c := range commits {
		if c.Date.IsZero() {
			t.Fatalf("commit %s has zero date", c.SHA)
		}
	}
}

func TestRewind(t *testing.T) {
	dir, base, env := newRepo(t)
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	ctx := context.Background()

	writeFile(t, filepath.Join(dir, "a.txt"), "v1\n")
	sha1, _, err := w.Checkpoint(ctx, "styr: checkpoint v1")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "v2\n")
	if _, _, err := w.Checkpoint(ctx, "styr: checkpoint v2"); err != nil {
		t.Fatal(err)
	}

	if err := w.Rewind(ctx, sha1); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v1\n" {
		t.Fatalf("a.txt = %q, want v1", got)
	}

	// A foreign sha, from an entirely unrelated repo with unique content
	// (so it cannot coincidentally hash to a real ancestor here), must be
	// refused.
	otherDir, _, otherEnv := newRepo(t)
	writeFile(t, filepath.Join(otherDir, "unique-foreign-file.txt"), "only in the other repo\n")
	runInDir(t, otherDir, otherEnv, "add", "-A")
	runInDir(t, otherDir, otherEnv, "commit", "-q", "-m", "foreign commit")
	otherSHA := strings.TrimSpace(runInDir(t, otherDir, otherEnv, "rev-parse", "HEAD"))
	if err := w.Rewind(ctx, otherSHA); err == nil || !errors.Is(err, ErrNotAncestor) {
		t.Fatalf("Rewind(foreign sha) err = %v, want ErrNotAncestor", err)
	}
}

func TestCommitFoldsCheckpoints(t *testing.T) {
	dir, base, env := newRepo(t)
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	ctx := context.Background()

	writeFile(t, filepath.Join(dir, "a.txt"), "1\n")
	if _, _, err := w.Checkpoint(ctx, "styr: checkpoint a"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "2\n")
	if _, _, err := w.Checkpoint(ctx, "styr: checkpoint b"); err != nil {
		t.Fatal(err)
	}

	author := Author{Name: "Jane Dev", Email: "jane@example.com"}
	sha, err := w.Commit(ctx, "feat: add a.txt", author)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha == "" {
		t.Fatal("empty sha")
	}

	commits, err := w.Log(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("commits = %d, want exactly 1: %+v", len(commits), commits)
	}
	if commits[0].Subject != "feat: add a.txt" {
		t.Fatalf("subject = %q", commits[0].Subject)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "2\n" {
		t.Fatalf("a.txt = %q, want final content 2", got)
	}
}

func TestCommitErrorsWhenClean(t *testing.T) {
	dir, base, env := newRepo(t)
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	_, err := w.Commit(context.Background(), "nothing to see", Author{Name: "A", Email: "a@example.com"})
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("err = %v, want ErrNothingToCommit", err)
	}
}
