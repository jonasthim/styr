package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// Loops is the repository for the loops table: until-done repetitions of a
// template, each iteration a run on the loop's one session.
type Loops struct{ d *DB }

// NewLoops constructs a Loops repository.
func NewLoops(d *DB) *Loops { return &Loops{d: d} }

const loopColumns = `id, template_id, session_id, origin, origin_ref, until_field, max_iterations,
	iteration, state, created_at, updated_at`

// defaultLoopListLimit caps List when the caller passes a non-positive limit.
const defaultLoopListLimit = 100

// Create inserts a new loop row. l.ID must already be set.
func (lp *Loops) Create(ctx context.Context, l domain.Loop) error {
	_, err := lp.d.ExecContext(ctx, `
		INSERT INTO loops (`+loopColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.TemplateID, optionalString(l.SessionID), l.Origin, l.OriginRef, l.UntilField,
		l.MaxIterations, l.Iteration, string(l.State), nowString(l.CreatedAt), nowString(l.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create loop: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create loop: %w", err)
	}
	return nil
}

func scanLoop(row interface{ Scan(dest ...any) error }) (*domain.Loop, error) {
	var (
		l                    domain.Loop
		sessionID            sql.NullString
		state                string
		createdAt, updatedAt string
	)
	if err := row.Scan(&l.ID, &l.TemplateID, &sessionID, &l.Origin, &l.OriginRef, &l.UntilField,
		&l.MaxIterations, &l.Iteration, &state, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	l.SessionID = nullString(sessionID)
	l.State = domain.LoopState(state)
	l.CreatedAt = parseTime(createdAt)
	l.UpdatedAt = parseTime(updatedAt)
	return &l, nil
}

// Get loads a loop by id.
func (lp *Loops) Get(ctx context.Context, id string) (*domain.Loop, error) {
	row := lp.d.QueryRowContext(ctx, `SELECT `+loopColumns+` FROM loops WHERE id = ?`, id)
	l, err := scanLoop(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get loop: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get loop: %w", err)
	}
	return l, nil
}

// List returns loops in the given state (every state when state is empty),
// newest first. Loops are unattended like runs, so List takes no visibility
// arguments.
func (lp *Loops) List(ctx context.Context, state string, limit int) ([]domain.Loop, error) {
	query := `SELECT ` + loopColumns + ` FROM loops`
	var args []any
	if state != "" {
		query += ` WHERE state = ?`
		args = append(args, state)
	}
	if limit <= 0 {
		limit = defaultLoopListLimit
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := lp.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list loops: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Loop
	for rows.Next() {
		l, err := scanLoop(rows)
		if err != nil {
			return nil, fmt.Errorf("scan loop: %w", err)
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// Update advances a loop: its state, the iteration it has reached and the
// session its runs share (nil leaves the stored session id alone, so an
// engine that only knows the new state cannot accidentally unlink it).
func (lp *Loops) Update(ctx context.Context, id string, state domain.LoopState, iteration int, sessionID *string) error {
	query := `UPDATE loops SET state = ?, iteration = ?, updated_at = ?`
	args := []any{string(state), iteration, nowString(time.Now())}
	if sessionID != nil {
		query += `, session_id = ?`
		args = append(args, *sessionID)
	}
	query += ` WHERE id = ?`
	args = append(args, id)

	res, err := lp.d.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update loop: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update loop: %w", domain.ErrNotFound)
	}
	return nil
}
