package gitops

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// checkpointPrefix marks a commit made by Checkpoint, so Commit knows
// which commits since BaseRef are safe to fold away.
const checkpointPrefix = "styr: checkpoint"

// Checkpoint stages and commits every change in the worktree under a
// fixed Styr identity (git add -A && git -c user.name=Styr -c
// user.email=styr@local commit --no-verify -q -m message). dirty is false,
// and sha is the current HEAD, when there was nothing to commit.
func (w Worktree) Checkpoint(ctx context.Context, message string) (sha string, dirty bool, err error) {
	env := w.env()
	if out, stderr, err := runGit(ctx, w.Path, env, "add", "-A"); err != nil {
		return "", false, fmt.Errorf("gitops: checkpoint add: %w: %s%s", err, out, stderr)
	}
	_, _, diffErr := runGit(ctx, w.Path, env, "diff", "--cached", "--quiet")
	if diffErr == nil {
		head, err := w.headSHA(ctx, env)
		if err != nil {
			return "", false, err
		}
		return head, false, nil
	}
	if !isCleanExit(diffErr) {
		return "", false, fmt.Errorf("gitops: checkpoint diff --cached: %w", diffErr)
	}
	if out, stderr, err := runGit(ctx, w.Path, env,
		"-c", "user.name=Styr", "-c", "user.email=styr@local",
		"commit", "--no-verify", "-q", "-m", message); err != nil {
		return "", false, fmt.Errorf("gitops: checkpoint commit: %w: %s%s", err, out, stderr)
	}
	sha, err = w.headSHA(ctx, env)
	if err != nil {
		return "", false, err
	}
	return sha, true, nil
}

// headSHA returns the worktree's current HEAD commit sha.
func (w Worktree) headSHA(ctx context.Context, env []string) (string, error) {
	out, stderr, err := runGit(ctx, w.Path, env, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitops: head sha: %w: %s", err, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// Log lists commits reachable from HEAD but not from since (default:
// BaseRef), newest first.
func (w Worktree) Log(ctx context.Context, since string) ([]Commit, error) {
	if since == "" {
		since = w.BaseRef
	}
	env := w.env()
	out, stderr, err := runGit(ctx, w.Path, env, "log", "--format=%H%x00%s%x00%cI", since+"..HEAD")
	if err != nil {
		return nil, fmt.Errorf("gitops: log: %w: %s", err, stderr)
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		t, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			t = time.Time{}
		}
		commits = append(commits, Commit{SHA: parts[0], Subject: parts[1], Date: t})
	}
	return commits, nil
}

// Rewind resets the worktree to sha, refusing (ErrNotAncestor) unless sha
// is an ancestor of the branch's current HEAD (which also refuses a sha
// unknown to this repo entirely).
func (w Worktree) Rewind(ctx context.Context, sha string) error {
	env := w.env()
	if _, _, err := runGit(ctx, w.Path, env, "merge-base", "--is-ancestor", sha, "HEAD"); err != nil {
		return fmt.Errorf("%w: %s", ErrNotAncestor, sha)
	}
	if out, stderr, err := runGit(ctx, w.Path, env, "reset", "--hard", sha); err != nil {
		return fmt.Errorf("gitops: rewind reset: %w: %s%s", err, out, stderr)
	}
	if out, stderr, err := runGit(ctx, w.Path, env, "clean", "-fd"); err != nil {
		return fmt.Errorf("gitops: rewind clean: %w: %s%s", err, out, stderr)
	}
	return nil
}

// Commit turns the worktree's changes into a single commit on top of
// BaseRef: any checkpoint commits already made since BaseRef are folded
// into it first (git reset --soft BaseRef), then everything is staged and
// committed as author. ErrNothingToCommit when there is nothing to commit.
func (w Worktree) Commit(ctx context.Context, message string, author Author) (string, error) {
	env := w.env()
	commits, err := w.Log(ctx, "")
	if err != nil {
		return "", err
	}
	hasCheckpoints := false
	for _, c := range commits {
		if strings.HasPrefix(c.Subject, checkpointPrefix) {
			hasCheckpoints = true
			break
		}
	}
	if hasCheckpoints {
		if out, stderr, err := runGit(ctx, w.Path, env, "reset", "--soft", w.BaseRef); err != nil {
			return "", fmt.Errorf("gitops: commit reset --soft: %w: %s%s", err, out, stderr)
		}
	}
	if out, stderr, err := runGit(ctx, w.Path, env, "add", "-A"); err != nil {
		return "", fmt.Errorf("gitops: commit add: %w: %s%s", err, out, stderr)
	}
	_, _, diffErr := runGit(ctx, w.Path, env, "diff", "--cached", "--quiet")
	if diffErr == nil {
		return "", ErrNothingToCommit
	}
	if !isCleanExit(diffErr) {
		return "", fmt.Errorf("gitops: commit diff --cached: %w", diffErr)
	}
	if out, stderr, err := runGit(ctx, w.Path, env,
		"-c", "user.name="+author.Name, "-c", "user.email="+author.Email,
		"commit", "-q", "-m", message); err != nil {
		return "", fmt.Errorf("gitops: commit: %w: %s%s", err, out, stderr)
	}
	return w.headSHA(ctx, env)
}
