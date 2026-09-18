package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// ReviewComments is the repository for the review_comments table: inline
// comments a human left on a session's diff, which become the next prompt
// when the review is sent.
type ReviewComments struct{ d *DB }

// NewReviewComments constructs a ReviewComments repository.
func NewReviewComments(d *DB) *ReviewComments { return &ReviewComments{d: d} }

const reviewCommentColumns = `id, session_id, path, line, side, body, author_id, created_at, sent_at`

// Create inserts a new review comment. c.ID must already be set.
func (r *ReviewComments) Create(ctx context.Context, c domain.ReviewComment) error {
	_, err := r.d.ExecContext(ctx, `
		INSERT INTO review_comments (`+reviewCommentColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.SessionID, c.Path, c.Line, string(c.Side), c.Body, c.AuthorID,
		nowString(c.CreatedAt), optionalTime(c.SentAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create review comment: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create review comment: %w", err)
	}
	return nil
}

// ListBySession returns every comment on a session, oldest first.
func (r *ReviewComments) ListBySession(ctx context.Context, sessionID string) ([]domain.ReviewComment, error) {
	rows, err := r.d.QueryContext(ctx,
		`SELECT `+reviewCommentColumns+` FROM review_comments WHERE session_id = ? ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list review comments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ReviewComment
	for rows.Next() {
		var (
			c       domain.ReviewComment
			side    string
			created string
			sent    sql.NullString
		)
		if err := rows.Scan(&c.ID, &c.SessionID, &c.Path, &c.Line, &side, &c.Body, &c.AuthorID, &created, &sent); err != nil {
			return nil, fmt.Errorf("scan review comment: %w", err)
		}
		c.Side = domain.ReviewSide(side)
		c.CreatedAt = parseTime(created)
		c.SentAt = nullTime(sent)
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkSent stamps sent_at on every listed comment that is not already
// sent. An empty id list is a no-op.
func (r *ReviewComments) MarkSent(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, nowString(time.Now()))
	for _, id := range ids {
		args = append(args, id)
	}
	query := `UPDATE review_comments SET sent_at = ? WHERE sent_at IS NULL AND id IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `)`
	if _, err := r.d.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("mark review comments sent: %w", err)
	}
	return nil
}

// Delete removes one comment, scoped to its session so a bad session id can
// never delete another session's comment.
func (r *ReviewComments) Delete(ctx context.Context, id, sessionID string) error {
	res, err := r.d.ExecContext(ctx, `DELETE FROM review_comments WHERE id = ? AND session_id = ?`, id, sessionID)
	if err != nil {
		return fmt.Errorf("delete review comment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete review comment: %w", domain.ErrNotFound)
	}
	return nil
}
