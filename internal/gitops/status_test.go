package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffSummary(t *testing.T) {
	dir, base, env := newRepo(t)
	writeFile(t, filepath.Join(dir, "mod.txt"), "line1\nline2\n")
	writeFile(t, filepath.Join(dir, "del.txt"), "bye\n")
	writeFile(t, filepath.Join(dir, "old.txt"), "content\n")
	runInDir(t, dir, env, "add", "-A")
	runInDir(t, dir, env, "commit", "-q", "-m", "seed files")
	base = strings.TrimSpace(runInDir(t, dir, env, "rev-parse", "HEAD"))

	// modified, left unstaged
	writeFile(t, filepath.Join(dir, "mod.txt"), "line1\nline2 changed\nline3\n")
	// deleted, left unstaged
	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	// renamed; staged, since rename pairing needs both sides in the index
	if err := os.Rename(filepath.Join(dir, "old.txt"), filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, env, "add", "-A", "--", "old.txt", "new.txt")
	// untracked addition, never staged
	writeFile(t, filepath.Join(dir, "untracked.txt"), "one\ntwo\nthree\n")

	w := Worktree{Path: dir, BaseRef: base, Env: env}
	sum, err := w.DiffSummary(context.Background())
	if err != nil {
		t.Fatalf("DiffSummary: %v", err)
	}

	byPath := map[string]FileChange{}
	for _, f := range sum.Files {
		byPath[f.Path] = f
	}
	if mod, ok := byPath["mod.txt"]; !ok || mod.Status != 'M' || mod.Add == 0 {
		t.Fatalf("mod.txt = %+v, ok=%v", mod, ok)
	}
	if del, ok := byPath["del.txt"]; !ok || del.Status != 'D' || del.Del == 0 {
		t.Fatalf("del.txt = %+v, ok=%v", del, ok)
	}
	if ren, ok := byPath["new.txt"]; !ok || ren.Status != 'R' || ren.OldPath != "old.txt" {
		t.Fatalf("new.txt = %+v, ok=%v", ren, ok)
	}
	if add, ok := byPath["untracked.txt"]; !ok || add.Status != 'A' || add.Add != 3 {
		t.Fatalf("untracked.txt = %+v, ok=%v", add, ok)
	}
	if sum.TotalAdd == 0 || sum.TotalDel == 0 {
		t.Fatalf("totals = +%d -%d, want both nonzero", sum.TotalAdd, sum.TotalDel)
	}
}

func TestStatusPorcelain(t *testing.T) {
	dir, _, env := newRepo(t)
	writeFile(t, filepath.Join(dir, "new.txt"), "hi\n")
	w := Worktree{Path: dir, Env: env}
	st, err := w.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	found := false
	for _, e := range st.Entries {
		if e.Path == "new.txt" && e.X == '?' && e.Y == '?' {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %+v, want new.txt as untracked", st.Entries)
	}
}
