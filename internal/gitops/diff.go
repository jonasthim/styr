package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LineType classifies one Hunk line.
type LineType string

const (
	LineContext LineType = "ctx"
	LineAdd     LineType = "add"
	LineDel     LineType = "del"
)

// Line is one line of a Hunk.
type Line struct {
	Type LineType
	// OldNo is the line's 1-based line number on the old side, 0 when the
	// line has no old-side counterpart (an add).
	OldNo int
	// NewNo is the line's 1-based line number on the new side, 0 when the
	// line has no new-side counterpart (a del).
	NewNo int
	Text  string
}

// Hunk is one unified-diff hunk.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []Line
}

// FileDiff is one file's diff against BaseRef, covering both committed and
// uncommitted changes.
type FileDiff struct {
	Path    string
	OldPath string
	Status  byte // 'A', 'M', 'D', or 'R'
	Binary  bool
	// Truncated is true when the underlying diff (or, for an untracked
	// file, its content) exceeded 2 MiB and was cut off.
	Truncated bool
	Hunks     []Hunk
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// FileDiff returns path's unified diff against BaseRef, covering both
// committed and uncommitted changes (git diff -M --no-color -U3 <base> --
// <path>). An untracked file's diff is synthesized as a single
// all-additions hunk from its content.
func (w Worktree) FileDiff(ctx context.Context, path string) (FileDiff, error) {
	st, err := w.Status(ctx)
	if err != nil {
		return FileDiff{}, err
	}
	for _, e := range st.Entries {
		if e.X == '?' && e.Y == '?' && e.Path == path {
			return w.untrackedFileDiff(path)
		}
	}
	out, stderr, err := runGit(ctx, w.Path, w.env(), "diff", "-M", "--no-color", "-U3", w.BaseRef, "--", path)
	if err != nil {
		return FileDiff{}, fmt.Errorf("gitops: file diff: %w: %s", err, stderr)
	}
	truncated := false
	if len(out) > maxDiffBytes {
		out = out[:maxDiffBytes]
		truncated = true
	}
	fd := parseUnifiedDiff(path, string(out))
	fd.Truncated = truncated
	return fd, nil
}

// untrackedFileDiff synthesizes a FileDiff for a file git does not track:
// one hunk of all-additions lines, or Binary: true when its content looks
// binary (a NUL byte in its first 8 KiB).
func (w Worktree) untrackedFileDiff(path string) (FileDiff, error) {
	fd := FileDiff{Path: path, Status: 'A'}
	data, err := os.ReadFile(filepath.Join(w.Path, path))
	if err != nil {
		return FileDiff{}, fmt.Errorf("gitops: read untracked file: %w", err)
	}
	probeLen := min(len(data), 8192)
	if bytes.IndexByte(data[:probeLen], 0) >= 0 {
		fd.Binary = true
		return fd, nil
	}
	if len(data) > maxDiffBytes {
		data = data[:maxDiffBytes]
		fd.Truncated = true
	}
	lines := splitLines(data)
	if len(lines) == 0 {
		return fd, nil
	}
	h := Hunk{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: len(lines)}
	for i, l := range lines {
		h.Lines = append(h.Lines, Line{Type: LineAdd, NewNo: i + 1, Text: l})
	}
	fd.Hunks = []Hunk{h}
	return fd, nil
}

// splitLines splits data on "\n", dropping the single trailing empty
// element a final newline produces.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// parseUnifiedDiff parses the output of
// `git diff -M --no-color -U3 <base> -- <path>` for a single file into a
// FileDiff.
func parseUnifiedDiff(path, out string) FileDiff {
	fd := FileDiff{Path: path, Status: 'M'}
	var cur *Hunk
	oldNo, newNo := 0, 0
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "rename from "):
			fd.OldPath = strings.TrimPrefix(line, "rename from ")
			fd.Status = 'R'
		case strings.HasPrefix(line, "rename to "):
			fd.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "new file mode"):
			fd.Status = 'A'
		case strings.HasPrefix(line, "deleted file mode"):
			fd.Status = 'D'
		case strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ"):
			fd.Binary = true
		case strings.HasPrefix(line, "@@ "):
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			fd.Hunks = append(fd.Hunks, Hunk{
				OldStart: atoiDefault(m[1], 0),
				OldLines: atoiDefault(m[2], 1),
				NewStart: atoiDefault(m[3], 0),
				NewLines: atoiDefault(m[4], 1),
			})
			cur = &fd.Hunks[len(fd.Hunks)-1]
			oldNo, newNo = cur.OldStart, cur.NewStart
		case cur != nil && strings.HasPrefix(line, `\ No newline`):
			// ignore
		case cur != nil && len(line) > 0 && (line[0] == ' ' || line[0] == '+' || line[0] == '-'):
			switch line[0] {
			case ' ':
				cur.Lines = append(cur.Lines, Line{Type: LineContext, OldNo: oldNo, NewNo: newNo, Text: line[1:]})
				oldNo++
				newNo++
			case '+':
				cur.Lines = append(cur.Lines, Line{Type: LineAdd, NewNo: newNo, Text: line[1:]})
				newNo++
			case '-':
				cur.Lines = append(cur.Lines, Line{Type: LineDel, OldNo: oldNo, Text: line[1:]})
				oldNo++
			}
		}
	}
	return fd
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
