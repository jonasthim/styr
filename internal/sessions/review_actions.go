// review_actions.go holds the review operations that change the repository or the session
// itself: folding the work into one commit, opening a pull request, rewinding to a checkpoint
// and discarding the worktree altogether.
package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/gitops"
)

// Sentinel errors for the two ways publishing a branch can fail for reasons the operator has
// to fix outside Styr. Both wrap domain.ErrConflict, so the API answers 409; it matches them
// with errors.Is to attach the documented error codes "gh_unavailable" and "no_remote".
var (
	ErrGHUnavailable = fmt.Errorf("%w: gh is not installed or not authenticated", domain.ErrConflict)
	ErrNoRemote      = fmt.Errorf("%w: the workspace has no git remote configured", domain.ErrConflict)
)

// busy reports whether a session is mid-turn or blocked on an approval, the two states in
// which rewriting its worktree underneath it would corrupt the run.
func busy(state domain.SessionState) bool {
	return state == domain.SessionRunning || state == domain.SessionWaiting
}

// Commit folds the session's work into a single commit authored by actor: any checkpoint
// commits made since the base are squashed into it (see gitops.Worktree.Commit) and the
// working tree is committed on top. Returns the new commit's sha.
func (s *Service) Commit(ctx context.Context, actor Actor, id, message string) (string, error) {
	sess, w, err := s.reviewTarget(ctx, actor, id)
	if err != nil {
		return "", err
	}
	if message == "" {
		return "", fmt.Errorf("%w: message is required", domain.ErrInvalid)
	}
	if busy(sess.State) {
		return "", fmt.Errorf("%w: the session is still working", domain.ErrConflict)
	}
	sha, err := w.Commit(ctx, message, s.authorFor(ctx, actor))
	switch {
	case errors.Is(err, gitops.ErrNothingToCommit):
		return "", fmt.Errorf("%w: there is nothing to commit", domain.ErrConflict)
	case err != nil:
		return "", fmt.Errorf("session commit: %w", err)
	}
	return sha, nil
}

// authorFor resolves the git author a commit is attributed to from actor's user record,
// falling back to Styr's own identity when the user cannot be loaded or carries no email.
func (s *Service) authorFor(ctx context.Context, actor Actor) gitops.Author {
	author := gitops.Author{Name: "Styr", Email: "styr@local"}
	if s.repos.Users == nil || actor.UserID == "" {
		return author
	}
	usr, err := s.repos.Users.GetByID(ctx, actor.UserID)
	if err != nil {
		s.logger.Error("commit author lookup", "user_id", actor.UserID, "error", err)
		return author
	}
	if usr.DisplayName != "" {
		author.Name = usr.DisplayName
	}
	if usr.Email != "" {
		author.Email = usr.Email
	}
	return author
}

// CreatePR pushes the session's branch to origin and opens a pull request for it via the gh
// CLI, returning the PR url. base defaults to the workspace's base branch (and, failing that,
// the checkout's current branch).
func (s *Service) CreatePR(ctx context.Context, actor Actor, id, title, body, base string) (string, error) {
	sess, w, err := s.reviewTarget(ctx, actor, id)
	if err != nil {
		return "", err
	}
	if title == "" {
		return "", fmt.Errorf("%w: title is required", domain.ErrInvalid)
	}
	ws, err := s.repos.Workspaces.Get(ctx, sess.WorkspaceID)
	if err != nil {
		return "", err
	}
	if base == "" {
		base, err = s.defaultBase(ctx, *ws)
		if err != nil {
			return "", err
		}
	}
	if err := w.Push(ctx); err != nil {
		if errors.Is(err, gitops.ErrNoRemote) {
			return "", ErrNoRemote
		}
		return "", fmt.Errorf("session push: %w", err)
	}
	url, err := w.CreatePR(ctx, title, body, base)
	if err != nil {
		if errors.Is(err, gitops.ErrGHUnavailable) {
			return "", ErrGHUnavailable
		}
		return "", fmt.Errorf("session pull request: %w", err)
	}
	return url, nil
}

// defaultBase is the branch a pull request targets when the caller names none.
func (s *Service) defaultBase(ctx context.Context, ws domain.Workspace) (string, error) {
	if ws.BaseBranch != "" {
		return ws.BaseBranch, nil
	}
	current, err := repoFor(ws).CurrentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve base branch: %w", err)
	}
	return current, nil
}

// Rewind resets the session's worktree to a checkpoint it recorded earlier. It refuses
// (domain.ErrConflict) while the session is running or waiting on an approval, since the CLI
// is holding files the reset would move underneath it.
func (s *Service) Rewind(ctx context.Context, actor Actor, id, checkpointID string) error {
	sess, w, err := s.reviewTarget(ctx, actor, id)
	if err != nil {
		return err
	}
	if busy(sess.State) {
		return fmt.Errorf("%w: the session is still working", domain.ErrConflict)
	}
	cp, err := s.repos.Checkpoints.Get(ctx, checkpointID)
	if err != nil {
		return err
	}
	if cp.SessionID != sess.ID {
		return fmt.Errorf("checkpoint %s: %w", checkpointID, domain.ErrNotFound)
	}
	if err := w.Rewind(ctx, cp.CommitSHA); err != nil {
		if errors.Is(err, gitops.ErrNotAncestor) {
			return fmt.Errorf("%w: the checkpoint is no longer on this branch", domain.ErrConflict)
		}
		return fmt.Errorf("session rewind: %w", err)
	}
	s.refreshDiffStats(ctx, sess, w)
	return nil
}

// refreshDiffStats recomputes and stores the session's diff counters after the worktree
// changed outside a turn (a rewind). Failures are logged, never propagated.
func (s *Service) refreshDiffStats(ctx context.Context, sess domain.Session, w gitops.Worktree) {
	sum, err := w.DiffSummary(ctx)
	if err != nil {
		s.logger.Error("refresh diff stats", "session_id", sess.ID, "error", err)
		return
	}
	if err := s.repos.Sessions.UpdateDiffStats(ctx, sess.ID, sum.TotalAdd, sum.TotalDel); err != nil {
		s.logger.Error("refresh diff stats", "session_id", sess.ID, "error", err)
	}
}

// Discard throws the session's work away: its process is closed, the worktree directory and
// its branch are removed, the worktree fields are cleared and the session is closed. It
// refuses while the session is running or waiting, like Rewind, and — since a pipeline's
// "worktree: shared" step leaves several sessions pointing at the same worktree — while another
// session is still open, running or waiting on that same worktree; it removes the worktree only
// once this is the last such session.
func (s *Service) Discard(ctx context.Context, actor Actor, id string) error {
	sess, _, err := s.reviewTarget(ctx, actor, id)
	if err != nil {
		return err
	}
	if busy(sess.State) {
		return fmt.Errorf("%w: the session is still working", domain.ErrConflict)
	}
	inUse, err := s.repos.Sessions.CountByWorktree(ctx, sess.Worktree, sess.ID, worktreeInUseStates)
	if err != nil {
		return err
	}
	if inUse > 0 {
		return fmt.Errorf("%w: worktree in use by another session", domain.ErrConflict)
	}
	ws, err := s.repos.Workspaces.Get(ctx, sess.WorkspaceID)
	if err != nil {
		return err
	}
	if err := s.closeLiveProcess(ctx, sess.ID); err != nil {
		return err
	}
	if err := repoFor(*ws).RemoveWorktree(ctx, sess.Worktree, true); err != nil {
		return fmt.Errorf("discard worktree: %w", err)
	}
	if err := s.repos.Sessions.SetWorktree(ctx, sess.ID, "", "", ""); err != nil {
		return err
	}
	if err := s.repos.Sessions.UpdateDiffStats(ctx, sess.ID, 0, 0); err != nil {
		return err
	}
	if sess.State == domain.SessionClosed {
		return nil
	}
	return s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionClosed)
}
