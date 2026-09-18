package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
)

// Checkpoints is the repository for the checkpoints table: the commits
// Styr made in a session's worktree after each turn, to rewind to.
type Checkpoints struct{ d *DB }

// NewCheckpoints constructs a Checkpoints repository.
func NewCheckpoints(d *DB) *Checkpoints { return &Checkpoints{d: d} }

const checkpointColumns = `id, session_id, commit_sha, turn, summary, created_at`

// Create inserts a new checkpoint row. c.ID must already be set.
func (c *Checkpoints) Create(ctx context.Context, cp domain.Checkpoint) error {
	_, err := c.d.ExecContext(ctx, `
		INSERT INTO checkpoints (`+checkpointColumns+`)
		VALUES (?, ?, ?, ?, ?, ?)`,
		cp.ID, cp.SessionID, cp.CommitSHA, cp.Turn, cp.Summary, nowString(cp.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create checkpoint: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create checkpoint: %w", err)
	}
	return nil
}

func scanCheckpoint(row interface{ Scan(dest ...any) error }) (*domain.Checkpoint, error) {
	var (
		cp      domain.Checkpoint
		created string
	)
	if err := row.Scan(&cp.ID, &cp.SessionID, &cp.CommitSHA, &cp.Turn, &cp.Summary, &created); err != nil {
		return nil, err
	}
	cp.CreatedAt = parseTime(created)
	return &cp, nil
}

// ListBySession returns a session's checkpoints, newest first.
func (c *Checkpoints) ListBySession(ctx context.Context, sessionID string) ([]domain.Checkpoint, error) {
	rows, err := c.d.QueryContext(ctx,
		`SELECT `+checkpointColumns+` FROM checkpoints WHERE session_id = ? ORDER BY created_at DESC, turn DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Checkpoint
	for rows.Next() {
		cp, err := scanCheckpoint(rows)
		if err != nil {
			return nil, fmt.Errorf("scan checkpoint: %w", err)
		}
		out = append(out, *cp)
	}
	return out, rows.Err()
}

// Get loads a checkpoint by id.
func (c *Checkpoints) Get(ctx context.Context, id string) (*domain.Checkpoint, error) {
	row := c.d.QueryRowContext(ctx, `SELECT `+checkpointColumns+` FROM checkpoints WHERE id = ?`, id)
	cp, err := scanCheckpoint(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get checkpoint: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get checkpoint: %w", err)
	}
	return cp, nil
}
