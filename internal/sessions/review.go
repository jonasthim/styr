// review.go is the review surface of a worktree session: the diff it produced, the inline
// comments a human left on that diff (which become the next prompt), and the checkpoints
// recorded after each turn. The actions that change the repository — commit, PR, rewind,
// discard — live in review_actions.go.
package sessions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/gitops"
)

// reviewHeader introduces the message SendReview composes from the unsent comments.
const reviewHeader = "Review comments on your changes (address each, then summarise what you changed):"

// ErrNoWorktree is returned by every review operation on a session that does not run in a git
// worktree (its workspace has worktrees turned off, or the worktree was discarded). It wraps
// domain.ErrInvalid, so the API answers 422.
var ErrNoWorktree = fmt.Errorf("%w: session has no worktree", domain.ErrInvalid)

// ReviewFile is one file in a ReviewDiff: gitops' own file change plus whether git considers
// the file binary (and so has no line-by-line diff to show).
type ReviewFile struct {
	Path    string
	OldPath string
	Status  byte // 'A', 'M', 'D' or 'R'
	Add     int
	Del     int
	Binary  bool
}

// ReviewDiff is a session worktree's whole diff against the commit it started from,
// uncommitted changes included.
type ReviewDiff struct {
	BaseRef  string
	Branch   string
	Files    []ReviewFile
	TotalAdd int
	TotalDel int
	// Dirty reports whether the worktree has uncommitted changes right now.
	Dirty bool
}

// reviewTarget loads a session actor may see and returns it together with its gitops
// worktree, or ErrNoWorktree when it has none.
func (s *Service) reviewTarget(ctx context.Context, actor Actor, id string) (domain.Session, gitops.Worktree, error) {
	sess, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return domain.Session{}, gitops.Worktree{}, err
	}
	if sess.Worktree == "" {
		return domain.Session{}, gitops.Worktree{}, ErrNoWorktree
	}
	return sess, worktreeOf(sess), nil
}

// Diff returns the session worktree's file-by-file diff against its base commit.
func (s *Service) Diff(ctx context.Context, actor Actor, id string) (ReviewDiff, error) {
	sess, w, err := s.reviewTarget(ctx, actor, id)
	if err != nil {
		return ReviewDiff{}, err
	}
	sum, err := w.DiffSummary(ctx)
	if err != nil {
		return ReviewDiff{}, fmt.Errorf("session diff: %w", err)
	}
	st, err := w.Status(ctx)
	if err != nil {
		return ReviewDiff{}, fmt.Errorf("session diff: %w", err)
	}
	return ReviewDiff{
		BaseRef:  sess.BaseRef,
		Branch:   sess.Branch,
		Files:    reviewFiles(ctx, w, sum.Files, st),
		TotalAdd: sum.TotalAdd,
		TotalDel: sum.TotalDel,
		Dirty:    len(st.Entries) > 0,
	}, nil
}

// reviewFiles turns gitops' file changes into ReviewFiles, flagging the binary ones. Only two
// kinds of entry can be binary — a tracked file, whose numstat counts git reports as zero
// added and zero removed lines, and an untracked one — so only those are re-checked with a
// per-file diff; every other entry already has real line counts and is text.
func reviewFiles(ctx context.Context, w gitops.Worktree, files []gitops.FileChange, st gitops.Status) []ReviewFile {
	untracked := make(map[string]bool, len(st.Entries))
	for _, e := range st.Entries {
		if e.X == '?' && e.Y == '?' {
			untracked[e.Path] = true
		}
	}
	out := make([]ReviewFile, 0, len(files))
	for _, f := range files {
		rf := ReviewFile{Path: f.Path, OldPath: f.OldPath, Status: f.Status, Add: f.Add, Del: f.Del}
		if untracked[f.Path] || (f.Add == 0 && f.Del == 0) {
			if fd, err := w.FileDiff(ctx, f.Path); err == nil {
				rf.Binary = fd.Binary
			}
		}
		out = append(out, rf)
	}
	return out
}

// FileDiff returns one file's unified diff against the session's base commit.
func (s *Service) FileDiff(ctx context.Context, actor Actor, id, path string) (gitops.FileDiff, error) {
	_, w, err := s.reviewTarget(ctx, actor, id)
	if err != nil {
		return gitops.FileDiff{}, err
	}
	if path == "" {
		return gitops.FileDiff{}, fmt.Errorf("%w: path is required", domain.ErrInvalid)
	}
	fd, err := w.FileDiff(ctx, path)
	if err != nil {
		return gitops.FileDiff{}, fmt.Errorf("session file diff: %w", err)
	}
	return fd, nil
}

// Patch returns the session worktree's whole diff as a patch, for download.
func (s *Service) Patch(ctx context.Context, actor Actor, id string) ([]byte, error) {
	_, w, err := s.reviewTarget(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	patch, err := w.Patch(ctx)
	if err != nil {
		return nil, fmt.Errorf("session patch: %w", err)
	}
	return patch, nil
}

// Comments returns every review comment on a session, oldest first, sent ones included.
func (s *Service) Comments(ctx context.Context, actor Actor, id string) ([]domain.ReviewComment, error) {
	if _, _, err := s.reviewTarget(ctx, actor, id); err != nil {
		return nil, err
	}
	list, err := s.repos.ReviewComments.ListBySession(ctx, id)
	if err != nil {
		return nil, err
	}
	s.fillAuthorNames(ctx, list)
	return list, nil
}

// fillAuthorNames resolves each comment's AuthorName from the users repository (display name,
// falling back to email), looking each author up at most once. A user that cannot be loaded
// simply keeps an empty name.
func (s *Service) fillAuthorNames(ctx context.Context, comments []domain.ReviewComment) {
	if s.repos.Users == nil {
		return
	}
	names := map[string]string{}
	for i := range comments {
		id := comments[i].AuthorID
		if id == "" {
			continue
		}
		name, known := names[id]
		if !known {
			if usr, err := s.repos.Users.GetByID(ctx, id); err == nil {
				name = usr.DisplayName
				if name == "" {
					name = usr.Email
				}
			}
			names[id] = name
		}
		comments[i].AuthorName = name
	}
}

// CommentInput is a new inline comment on a session's diff.
type CommentInput struct {
	Path string
	Line int
	Side domain.ReviewSide
	Body string
}

// AddComment records one inline comment. It is not sent to the session until SendReview.
func (s *Service) AddComment(ctx context.Context, actor Actor, id string, in CommentInput) (domain.ReviewComment, error) {
	if _, _, err := s.reviewTarget(ctx, actor, id); err != nil {
		return domain.ReviewComment{}, err
	}
	if in.Path == "" {
		return domain.ReviewComment{}, fmt.Errorf("%w: path is required", domain.ErrInvalid)
	}
	if strings.TrimSpace(in.Body) == "" {
		return domain.ReviewComment{}, fmt.Errorf("%w: body is required", domain.ErrInvalid)
	}
	if in.Side == "" {
		in.Side = domain.ReviewSideNew
	}
	if !domain.ValidReviewSide(in.Side) {
		return domain.ReviewComment{}, fmt.Errorf(`%w: side must be "old" or "new"`, domain.ErrInvalid)
	}
	if in.Line < 0 {
		return domain.ReviewComment{}, fmt.Errorf("%w: line must not be negative", domain.ErrInvalid)
	}
	c := domain.ReviewComment{
		ID:        uuid.NewString(),
		SessionID: id,
		Path:      in.Path,
		Line:      in.Line,
		Side:      in.Side,
		Body:      in.Body,
		AuthorID:  actor.UserID,
		CreatedAt: time.Now(),
	}
	if err := s.repos.ReviewComments.Create(ctx, c); err != nil {
		return domain.ReviewComment{}, err
	}
	one := []domain.ReviewComment{c}
	s.fillAuthorNames(ctx, one)
	return one[0], nil
}

// DeleteComment removes one of a session's review comments.
func (s *Service) DeleteComment(ctx context.Context, actor Actor, id, commentID string) error {
	if _, _, err := s.reviewTarget(ctx, actor, id); err != nil {
		return err
	}
	return s.repos.ReviewComments.Delete(ctx, commentID, id)
}

// SendReview composes every unsent comment into one user message, sends it to the session
// (starting or resuming its process the same way an ordinary message does) and marks the
// comments sent. domain.ErrInvalid when there is nothing unsent to send.
func (s *Service) SendReview(ctx context.Context, actor Actor, id string) error {
	if _, _, err := s.reviewTarget(ctx, actor, id); err != nil {
		return err
	}
	all, err := s.repos.ReviewComments.ListBySession(ctx, id)
	if err != nil {
		return err
	}
	var unsent []domain.ReviewComment
	for _, c := range all {
		if c.SentAt == nil {
			unsent = append(unsent, c)
		}
	}
	if len(unsent) == 0 {
		return fmt.Errorf("%w: there are no unsent review comments", domain.ErrInvalid)
	}
	if err := s.Send(ctx, actor, id, formatReview(unsent)); err != nil {
		return err
	}
	ids := make([]string, 0, len(unsent))
	for _, c := range unsent {
		ids = append(ids, c.ID)
	}
	return s.repos.ReviewComments.MarkSent(ctx, ids)
}

// formatReview renders comments as the review message the CLI receives:
//
//	Review comments on your changes (address each, then summarise what you changed):
//	- src/auth.go:42 (new): please handle the error
func formatReview(comments []domain.ReviewComment) string {
	var b strings.Builder
	b.WriteString(reviewHeader)
	for _, c := range comments {
		fmt.Fprintf(&b, "\n- %s:%d (%s): %s", c.Path, c.Line, c.Side, c.Body)
	}
	return b.String()
}

// Checkpoints returns a session's checkpoints, newest first.
func (s *Service) Checkpoints(ctx context.Context, actor Actor, id string) ([]domain.Checkpoint, error) {
	if _, _, err := s.reviewTarget(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.repos.Checkpoints.ListBySession(ctx, id)
}
