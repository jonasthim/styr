package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// StatusEntry is one entry of `git status --porcelain=v1`.
type StatusEntry struct {
	X, Y byte
	Path string
	// OrigPath is set when X or Y is 'R' or 'C' (rename/copy).
	OrigPath string
}

// Status is the worktree's porcelain status.
type Status struct {
	Entries []StatusEntry
}

// Status runs `git status --porcelain=v1 -z` and parses its output.
func (w Worktree) Status(ctx context.Context) (Status, error) {
	out, stderr, err := runGit(ctx, w.Path, w.env(), "status", "--porcelain=v1", "-z")
	if err != nil {
		return Status{}, fmt.Errorf("gitops: status: %w: %s", err, stderr)
	}
	var st Status
	tokens := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if len(tok) < 3 {
			continue
		}
		e := StatusEntry{X: tok[0], Y: tok[1], Path: tok[3:]}
		if e.X == 'R' || e.X == 'C' || e.Y == 'R' || e.Y == 'C' {
			i++
			if i < len(tokens) {
				e.OrigPath = tokens[i]
			}
		}
		st.Entries = append(st.Entries, e)
	}
	return st, nil
}

// FileChange is one file's entry in a DiffSummary.
type FileChange struct {
	Path string
	// OldPath is set when Status == 'R'.
	OldPath string
	Status  byte // 'A', 'M', 'D', or 'R'
	Add     int
	Del     int
}

// Summary is DiffSummary's result: every file changed between BaseRef and
// the worktree's current state, committed or not.
type Summary struct {
	Files    []FileChange
	TotalAdd int
	TotalDel int
}

// DiffSummary reports every file changed between BaseRef and the current
// working tree, tracked and untracked alike. Tracked changes (committed or
// uncommitted, staged or not) come from `git diff --numstat -M`; untracked
// additions come from `git status` and are counted by line count.
func (w Worktree) DiffSummary(ctx context.Context) (Summary, error) {
	env := w.env()
	out, stderr, err := runGit(ctx, w.Path, env, "diff", "--numstat", "-M", "--no-color", w.BaseRef)
	if err != nil {
		return Summary{}, fmt.Errorf("gitops: diff summary: %w: %s", err, stderr)
	}
	var sum Summary
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fc, err := w.parseNumstatLine(ctx, env, line)
		if err != nil {
			return Summary{}, err
		}
		sum.Files = append(sum.Files, fc)
		sum.TotalAdd += fc.Add
		sum.TotalDel += fc.Del
	}
	st, err := w.Status(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, e := range st.Entries {
		if e.X != '?' || e.Y != '?' {
			continue
		}
		add, err := countLines(filepath.Join(w.Path, e.Path))
		if err != nil {
			continue // unreadable (e.g. removed since status ran); skip
		}
		sum.Files = append(sum.Files, FileChange{Path: e.Path, Status: 'A', Add: add})
		sum.TotalAdd += add
	}
	return sum, nil
}

// parseNumstatLine parses one tab-separated line of `git diff --numstat`
// output into a FileChange, resolving its status by checking whether the
// path existed at BaseRef and whether it exists in the working tree now
// (numstat itself carries no status letter, only add/del counts and, for
// a rename, the "old => new" name field).
func (w Worktree) parseNumstatLine(ctx context.Context, env []string, line string) (FileChange, error) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return FileChange{}, fmt.Errorf("gitops: unexpected numstat line %q", line)
	}
	fc := FileChange{Add: parseCount(parts[0]), Del: parseCount(parts[1])}
	nameField := parts[2]
	if strings.Contains(nameField, " => ") {
		old, cur := splitRename(nameField)
		fc.Path, fc.OldPath, fc.Status = cur, old, 'R'
		return fc, nil
	}
	fc.Path = nameField
	oldExists := w.existsAtRef(ctx, env, w.BaseRef, nameField)
	newExists := fileExists(filepath.Join(w.Path, nameField))
	switch {
	case !oldExists && newExists:
		fc.Status = 'A'
	case oldExists && !newExists:
		fc.Status = 'D'
	default:
		fc.Status = 'M'
	}
	return fc, nil
}

// existsAtRef reports whether path exists in the tree at ref.
func (w Worktree) existsAtRef(ctx context.Context, env []string, ref, path string) bool {
	_, _, err := runGit(ctx, w.Path, env, "cat-file", "-e", ref+":"+path)
	return err == nil
}

func parseCount(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// splitRename splits a numstat rename name field ("old => new", or a path
// with an embedded "{old => new}" segment for a partial-directory rename)
// into its old and new paths.
func splitRename(field string) (oldPath, newPath string) {
	if i := strings.Index(field, "{"); i >= 0 {
		if j := strings.Index(field[i:], "}"); j >= 0 {
			j += i
			prefix, inner, suffix := field[:i], field[i+1:j], field[j+1:]
			if parts := strings.SplitN(inner, " => ", 2); len(parts) == 2 {
				return prefix + parts[0] + suffix, prefix + parts[1] + suffix
			}
		}
	}
	if parts := strings.SplitN(field, " => ", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return field, field
}

// countLines counts path's lines for use as an additions count: each
// newline-terminated line, plus one more for a final line with no
// trailing newline. An empty file counts as zero lines.
func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	n := bytes.Count(data, []byte("\n"))
	if data[len(data)-1] != '\n' {
		n++
	}
	return n, nil
}
