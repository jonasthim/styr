// worktree.go creates and maintains the git worktree a session runs in when its workspace
// opts into worktrees: one worktree per session under <workspace>/.styr/worktrees/<session
// id>, on its own branch, plus the after-every-turn checkpoint commit and diff stats that
// make the Review tab (and the sessions list's diff badge) possible.
package sessions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/gitops"
)

// worktreesSubdir is the workspace-relative directory every session
// worktree lives under. internal/gitops adds it to the checkout's
// .git/info/exclude so the main checkout never sees the worktrees as
// untracked files.
const worktreesSubdir = ".styr/worktrees"

// branchSlugMax bounds the title-derived part of a session branch name.
const branchSlugMax = 40

// shortIDLen is how many leading characters of the session id go into its
// branch name.
const shortIDLen = 8

// worktreeInUseStates are the session states that count as "still using its
// worktree" for Discard's shared-worktree conflict check (see
// review_actions.go): a session in any other state (closed, failed) is
// done with its worktree even though the row still names it.
var worktreeInUseStates = []domain.SessionState{domain.SessionOpen, domain.SessionRunning, domain.SessionWaiting}

// repoFor returns the gitops handle for a workspace's own checkout.
func repoFor(ws domain.Workspace) gitops.Repo { return gitops.Repo{Path: ws.Path} }

// worktreeOf returns the gitops handle for a session's worktree. Only
// meaningful when sess.Worktree is set.
func worktreeOf(sess domain.Session) gitops.Worktree {
	return gitops.Worktree{Path: sess.Worktree, Branch: sess.Branch, BaseRef: sess.BaseRef}
}

// worktreeDir is where sess's worktree lives inside ws.
func worktreeDir(ws domain.Workspace, sessionID string) string {
	return filepath.Join(ws.Path, filepath.FromSlash(worktreesSubdir), sessionID)
}

// branchName builds a session's branch: "styr/<first 8 of id>-<slug of title>", matching the
// name shape internal/gitops accepts (lowercase letters, digits and dashes only).
func branchName(id, title string) string {
	short := slugify(id, shortIDLen)
	if short == "" {
		short = "session"
	}
	name := short
	if slug := slugify(title, branchSlugMax); slug != "" {
		name = short + "-" + slug
	}
	return "styr/" + name
}

// slugify lowercases s, collapses every run of characters that is not a lowercase letter or
// digit into a single dash, trims dashes from both ends and cuts the result to max characters
// (trimming any dash the cut exposes).
func slugify(s string, max int) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > max {
		out = out[:max]
	}
	return strings.Trim(out, "-")
}

// createWorktree adds sess's git worktree to ws's checkout, records its path, branch and base
// commit on the session row, and returns the updated session. The base is the workspace's
// configured base branch, or the checkout's current branch when it has none.
func (s *Service) createWorktree(ctx context.Context, sess domain.Session, ws domain.Workspace) (domain.Session, error) {
	repo := repoFor(ws)
	base := ws.BaseBranch
	if base == "" {
		current, err := repo.CurrentBranch(ctx)
		if err != nil {
			return sess, fmt.Errorf("create worktree: %w", err)
		}
		base = current
	}

	dir := worktreeDir(ws, sess.ID)
	branch := branchName(sess.ID, sess.Title)
	if err := repo.AddWorktree(ctx, dir, branch, base); err != nil {
		return sess, fmt.Errorf("create worktree: %w", err)
	}

	baseRef, err := baseCommitOf(ctx, dir, branch)
	if err != nil {
		_ = repo.RemoveWorktree(ctx, dir, true)
		return sess, err
	}
	if err := s.repos.Sessions.SetWorktree(ctx, sess.ID, dir, branch, baseRef); err != nil {
		_ = repo.RemoveWorktree(ctx, dir, true)
		return sess, err
	}
	sess.Worktree, sess.Branch, sess.BaseRef = dir, branch, baseRef
	return sess, nil
}

// baseCommitOf resolves the commit a freshly created worktree starts from. gitops exposes no
// bare rev-parse, but Checkpoint on a clean worktree commits nothing and reports HEAD — and a
// worktree `git worktree add` just created is always clean.
func baseCommitOf(ctx context.Context, dir, branch string) (string, error) {
	w := gitops.Worktree{Path: dir, Branch: branch}
	sha, _, err := w.Checkpoint(ctx, "styr: worktree base")
	if err != nil {
		return "", fmt.Errorf("resolve worktree base commit: %w", err)
	}
	return sha, nil
}

// attachWorktree records sess as running in path, an existing git worktree
// another session already created, instead of making a fresh one: the
// "worktree: shared" step of a pipeline continuing the previous step's
// work (see CreateInput.WorktreePath). path must already exist as a
// registered worktree under ws's worktrees root; its branch comes from
// git, and its base commit from the session that originally created it
// (git worktree list carries no base information of its own).
func (s *Service) attachWorktree(ctx context.Context, sess domain.Session, ws domain.Workspace, path string) (domain.Session, error) {
	if err := validateWorktreePath(ws, path); err != nil {
		return sess, err
	}
	info, err := repoFor(ws).WorktreeInfo(ctx, path)
	if err != nil {
		if errors.Is(err, gitops.ErrNotWorktree) {
			return sess, fmt.Errorf("%w: %s is not a git worktree of this workspace", domain.ErrInvalid, path)
		}
		return sess, fmt.Errorf("attach worktree: %w", err)
	}
	owner, err := s.repos.Sessions.GetByWorktree(ctx, path)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return sess, fmt.Errorf("%w: %s has no owning session to take a base commit from", domain.ErrInvalid, path)
		}
		return sess, fmt.Errorf("attach worktree: %w", err)
	}
	if err := s.repos.Sessions.SetWorktree(ctx, sess.ID, path, info.Branch, owner.BaseRef); err != nil {
		return sess, err
	}
	if err := s.repos.Sessions.SetWorktreeShared(ctx, sess.ID, true); err != nil {
		return sess, err
	}
	sess.Worktree, sess.Branch, sess.BaseRef, sess.WorktreeShared = path, info.Branch, owner.BaseRef, true
	return sess, nil
}

// validateWorktreePath rejects a CreateInput.WorktreePath that is not a
// directory strictly inside ws's own worktrees root
// (<workspace>/.styr/worktrees/): outside that root entirely (an escape,
// or an unrelated absolute path), the root itself, or a path that simply
// does not exist. It does not check with git; attachWorktree's
// gitops.Repo.WorktreeInfo call does that.
func validateWorktreePath(ws domain.Workspace, path string) error {
	root, err := filepath.Abs(filepath.Join(ws.Path, filepath.FromSlash(worktreesSubdir)))
	if err != nil {
		return fmt.Errorf("%w: cannot resolve the workspace's worktrees directory", domain.ErrInvalid)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve worktree path %q", domain.ErrInvalid, path)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: worktree path %q is not under the workspace's worktrees directory", domain.ErrInvalid, path)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: worktree path %q does not exist", domain.ErrInvalid, path)
	}
	return nil
}

// afterResult runs the per-turn worktree bookkeeping once a turn has finished: an optional
// checkpoint commit (recorded as a checkpoints row when it actually committed something) and
// the refreshed diff stats, which it returns for session.stats. A session with no worktree is
// a no-op. Every failure is logged rather than propagated: a git problem must not derail the
// transcript.
func (s *Service) afterResult(ctx context.Context, sess domain.Session, turn int) (add, del int) {
	if sess.Worktree == "" {
		return 0, 0
	}
	ws, err := s.repos.Workspaces.Get(ctx, sess.WorkspaceID)
	if err != nil {
		s.logger.Error("worktree bookkeeping: load workspace", "session_id", sess.ID, "error", err)
		return 0, 0
	}
	w := worktreeOf(sess)
	if ws.AutoCheckpoint {
		s.checkpoint(ctx, sess, w, turn)
	}
	sum, err := w.DiffSummary(ctx)
	if err != nil {
		s.logger.Error("worktree bookkeeping: diff summary", "session_id", sess.ID, "error", err)
		return 0, 0
	}
	if err := s.repos.Sessions.UpdateDiffStats(ctx, sess.ID, sum.TotalAdd, sum.TotalDel); err != nil {
		s.logger.Error("worktree bookkeeping: update diff stats", "session_id", sess.ID, "error", err)
	}
	return sum.TotalAdd, sum.TotalDel
}

// checkpoint commits whatever the turn changed and records it, doing nothing when the
// worktree is clean.
func (s *Service) checkpoint(ctx context.Context, sess domain.Session, w gitops.Worktree, turn int) {
	message := fmt.Sprintf("styr: checkpoint after turn %d", turn)
	sha, dirty, err := w.Checkpoint(ctx, message)
	if err != nil {
		s.logger.Error("worktree checkpoint", "session_id", sess.ID, "error", err)
		return
	}
	if !dirty {
		return
	}
	cp := domain.Checkpoint{
		ID:        uuid.NewString(),
		SessionID: sess.ID,
		CommitSHA: sha,
		Turn:      turn,
		Summary:   message,
		CreatedAt: time.Now(),
	}
	if err := s.repos.Checkpoints.Create(ctx, cp); err != nil {
		s.logger.Error("record checkpoint", "session_id", sess.ID, "error", err)
	}
}
