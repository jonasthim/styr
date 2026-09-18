package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushToBareOrigin(t *testing.T) {
	dir, base, env := newRepo(t)
	bareDir := t.TempDir()
	runInDir(t, bareDir, env, "init", "-q", "--bare")
	runInDir(t, dir, env, "remote", "add", "origin", bareDir)

	w := Worktree{Path: dir, Branch: "main", BaseRef: base, Env: env}
	if err := w.Push(context.Background()); err != nil {
		t.Fatalf("Push: %v", err)
	}
	out := runInDir(t, bareDir, env, "rev-parse", "main")
	if strings.TrimSpace(out) != base {
		t.Fatalf("bare origin main = %q, want %s", out, base)
	}
}

func TestPushErrNoRemote(t *testing.T) {
	dir, base, env := newRepo(t)
	w := Worktree{Path: dir, Branch: "main", BaseRef: base, Env: env}
	if err := w.Push(context.Background()); !errors.Is(err, ErrNoRemote) {
		t.Fatalf("err = %v, want ErrNoRemote", err)
	}
}

const ghStubOK = "#!/bin/sh\n" +
	"if [ \"$1 $2\" = \"auth status\" ]; then exit 0; fi\n" +
	"if [ \"$1 $2\" = \"pr create\" ]; then echo https://github.com/example/repo/pull/1; exit 0; fi\n" +
	"exit 1\n"

func writeStubGH(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCreatePR(t *testing.T) {
	dir, base, _ := newRepo(t)
	ghDir := writeStubGH(t, ghStubOK)
	env := []string{"PATH=" + ghDir, "HOME=" + t.TempDir()}

	w := Worktree{Path: dir, Branch: "styr/s1-x", BaseRef: base, Env: env}
	url, err := w.CreatePR(context.Background(), "title", "body", "main")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if url != "https://github.com/example/repo/pull/1" {
		t.Fatalf("url = %q", url)
	}
}

func TestCreatePRErrGHUnavailable(t *testing.T) {
	dir, base, _ := newRepo(t)
	// An empty PATH means gh cannot be found, regardless of what is
	// installed on the host running the test.
	env := []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}
	w := Worktree{Path: dir, Branch: "styr/s1-x", BaseRef: base, Env: env}
	_, err := w.CreatePR(context.Background(), "t", "b", "main")
	if !errors.Is(err, ErrGHUnavailable) {
		t.Fatalf("err = %v, want ErrGHUnavailable", err)
	}
}

func TestPatchNonEmpty(t *testing.T) {
	dir, base, env := newRepo(t)
	writeFile(t, filepath.Join(dir, "README.md"), "hello changed\n")
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	patch, err := w.Patch(context.Background())
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if len(patch) == 0 {
		t.Fatal("empty patch")
	}
	if !strings.Contains(string(patch), "README.md") {
		t.Fatalf("patch missing README.md: %s", patch)
	}
}
