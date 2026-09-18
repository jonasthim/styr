package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileDiffHunkLineNumbers(t *testing.T) {
	dir, base, env := newRepo(t)
	writeFile(t, filepath.Join(dir, "f.txt"), "a\nb\nc\nd\ne\n")
	runInDir(t, dir, env, "add", "-A")
	runInDir(t, dir, env, "commit", "-q", "-m", "seed")
	base = strings.TrimSpace(runInDir(t, dir, env, "rev-parse", "HEAD"))

	writeFile(t, filepath.Join(dir, "f.txt"), "a\nb\nX\nd\ne\n")

	w := Worktree{Path: dir, BaseRef: base, Env: env}
	fd, err := w.FileDiff(context.Background(), "f.txt")
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if fd.Binary || fd.Truncated {
		t.Fatalf("fd = %+v, want text, non-truncated", fd)
	}
	if len(fd.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(fd.Hunks))
	}
	h := fd.Hunks[0]
	var gotDel, gotAdd *Line
	for i := range h.Lines {
		switch h.Lines[i].Type {
		case LineDel:
			if gotDel == nil {
				gotDel = &h.Lines[i]
			}
		case LineAdd:
			if gotAdd == nil {
				gotAdd = &h.Lines[i]
			}
		}
	}
	if gotDel == nil || gotDel.Text != "c" || gotDel.OldNo != 3 {
		t.Fatalf("del line = %+v, want {c, old 3}", gotDel)
	}
	if gotAdd == nil || gotAdd.Text != "X" || gotAdd.NewNo != 3 {
		t.Fatalf("add line = %+v, want {X, new 3}", gotAdd)
	}
	// Unchanged context lines must also carry both old and new numbers.
	var ctxLine *Line
	for i := range h.Lines {
		if h.Lines[i].Type == LineContext && h.Lines[i].Text == "a" {
			ctxLine = &h.Lines[i]
		}
	}
	if ctxLine == nil || ctxLine.OldNo != 1 || ctxLine.NewNo != 1 {
		t.Fatalf("context line 'a' = %+v, want {old 1, new 1}", ctxLine)
	}
}

func TestFileDiffUntrackedSynthesized(t *testing.T) {
	dir, base, env := newRepo(t)
	writeFile(t, filepath.Join(dir, "new.txt"), "x\ny\nz\n")
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	fd, err := w.FileDiff(context.Background(), "new.txt")
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if fd.Status != 'A' || fd.Binary {
		t.Fatalf("fd = %+v, want status A, not binary", fd)
	}
	if len(fd.Hunks) != 1 || len(fd.Hunks[0].Lines) != 3 {
		t.Fatalf("hunks = %+v, want 1 hunk of 3 add lines", fd.Hunks)
	}
	for i, l := range fd.Hunks[0].Lines {
		if l.Type != LineAdd || l.NewNo != i+1 {
			t.Fatalf("line %d = %+v, want add with new_no %d", i, l, i+1)
		}
	}
}

func TestFileDiffBinaryTracked(t *testing.T) {
	dir, base, env := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0x00, 0x01, 0x02, 'a'}, 0o644); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, env, "add", "-A")
	runInDir(t, dir, env, "commit", "-q", "-m", "seed binary")
	base = strings.TrimSpace(runInDir(t, dir, env, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0x00, 0x01, 0x02, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}

	w := Worktree{Path: dir, BaseRef: base, Env: env}
	fd, err := w.FileDiff(context.Background(), "bin.dat")
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if !fd.Binary {
		t.Fatalf("fd.Binary = false, want true")
	}
}

func TestFileDiffBinaryUntracked(t *testing.T) {
	dir, base, env := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.dat"), []byte{0x00, 0x01, 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	w := Worktree{Path: dir, BaseRef: base, Env: env}
	fd, err := w.FileDiff(context.Background(), "new.dat")
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if !fd.Binary {
		t.Fatalf("fd.Binary = false, want true")
	}
}
