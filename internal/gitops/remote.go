package gitops

import (
	"context"
	"fmt"
	"strings"
)

// Push pushes the worktree's branch to origin, creating the upstream
// tracking ref (git push -u origin <branch>). It never force-pushes.
// ErrNoRemote when the repo has no remotes configured.
func (w Worktree) Push(ctx context.Context) error {
	env := w.env()
	out, stderr, err := runGit(ctx, w.Path, env, "remote")
	if err != nil {
		return fmt.Errorf("gitops: list remotes: %w: %s", err, stderr)
	}
	if strings.TrimSpace(string(out)) == "" {
		return ErrNoRemote
	}
	if out, stderr, err := runGit(ctx, w.Path, env, "push", "-u", "origin", w.Branch); err != nil {
		return fmt.Errorf("gitops: push: %w: %s%s", err, out, stderr)
	}
	return nil
}

// CreatePR opens a pull request for the worktree's branch via the gh CLI
// (gh pr create --head <branch> --base <base> --title --body), returning
// the PR URL parsed from its stdout. ErrGHUnavailable when gh is not on
// PATH or `gh auth status` fails (not authenticated).
func (w Worktree) CreatePR(ctx context.Context, title, body, base string) (string, error) {
	env := w.env()
	if _, err := lookPath(env, "gh"); err != nil {
		return "", ErrGHUnavailable
	}
	if _, stderr, err := runCmd(ctx, w.Path, env, "gh", "auth", "status"); err != nil {
		return "", fmt.Errorf("%w: %s", ErrGHUnavailable, strings.TrimSpace(stderr))
	}
	out, stderr, err := runCmd(ctx, w.Path, env, "gh", "pr", "create",
		"--head", w.Branch, "--base", base, "--title", title, "--body", body)
	if err != nil {
		return "", fmt.Errorf("gitops: create pr: %w: %s", err, stderr)
	}
	url := lastNonEmptyLine(string(out))
	if url == "" {
		return "", fmt.Errorf("gitops: create pr: no url in output: %q", out)
	}
	return url, nil
}

// Patch returns the worktree's full diff against BaseRef (committed and
// uncommitted changes) as bytes, for download.
func (w Worktree) Patch(ctx context.Context) ([]byte, error) {
	env := w.env()
	out, stderr, err := runGit(ctx, w.Path, env, "diff", w.BaseRef)
	if err != nil {
		return nil, fmt.Errorf("gitops: patch: %w: %s", err, stderr)
	}
	return out, nil
}
