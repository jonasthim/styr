package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// Runs is the repository for the runs table: unattended sessions started
// from a template.
type Runs struct{ d *DB }

// NewRuns constructs a Runs repository.
func NewRuns(d *DB) *Runs { return &Runs{d: d} }

const runColumns = `id, session_id, template_id, trigger_id, delivery_id, origin, started_at, finished_at,
	outcome, report, summary, cost_usd`

// defaultRunListLimit caps List when the caller passes a non-positive
// RunFilter.Limit.
const defaultRunListLimit = 100

// Create inserts a new run row. r.ID must already be set.
func (rp *Runs) Create(ctx context.Context, r domain.Run) error {
	_, err := rp.d.ExecContext(ctx, `
		INSERT INTO runs (`+runColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.SessionID, optionalString(r.TemplateID), optionalString(r.TriggerID), optionalString(r.DeliveryID),
		r.Origin, nowString(r.StartedAt), optionalTime(r.FinishedAt), string(r.Outcome), string(r.Report),
		r.Summary, r.CostUSD)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create run: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func optionalString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func scanRun(row interface{ Scan(dest ...any) error }) (*domain.Run, error) {
	var (
		r                     domain.Run
		templateID, triggerID sql.NullString
		deliveryID            sql.NullString
		startedAt             string
		finishedAt            sql.NullString
		outcome, report       string
	)
	if err := row.Scan(&r.ID, &r.SessionID, &templateID, &triggerID, &deliveryID, &r.Origin,
		&startedAt, &finishedAt, &outcome, &report, &r.Summary, &r.CostUSD); err != nil {
		return nil, err
	}
	r.TemplateID = nullString(templateID)
	r.TriggerID = nullString(triggerID)
	r.DeliveryID = nullString(deliveryID)
	r.StartedAt = parseTime(startedAt)
	r.FinishedAt = nullTime(finishedAt)
	r.Outcome = domain.RunOutcome(outcome)
	r.Report = json.RawMessage(report)
	return &r, nil
}

// Get loads a run by id.
func (rp *Runs) Get(ctx context.Context, id string) (*domain.Run, error) {
	row := rp.d.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id)
	r, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get run: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get run: %w", err)
	}
	return r, nil
}

// GetBySession loads the run tied to sessionID.
func (rp *Runs) GetBySession(ctx context.Context, sessionID string) (*domain.Run, error) {
	row := rp.d.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE session_id = ?`, sessionID)
	r, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get run by session: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get run by session: %w", err)
	}
	return r, nil
}

// List returns runs matching filter, newest first (matching the
// runs_started_idx index). Runs are visible to everyone per the v0.1
// decision, so List takes no visibility arguments.
func (rp *Runs) List(ctx context.Context, filter domain.RunFilter) ([]domain.Run, error) {
	query := `SELECT ` + runColumns + ` FROM runs WHERE 1 = 1`
	var args []any
	if filter.Outcome != "" {
		query += ` AND outcome = ?`
		args = append(args, filter.Outcome)
	}
	if filter.TriggerID != "" {
		query += ` AND trigger_id = ?`
		args = append(args, filter.TriggerID)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultRunListLimit
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := rp.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Finish closes out a run: sets finished_at to now, its outcome, the
// parsed structured report, the summary line and the accumulated cost.
func (rp *Runs) Finish(ctx context.Context, id string, outcome domain.RunOutcome, report json.RawMessage, summary string, costUSD float64) error {
	res, err := rp.d.ExecContext(ctx, `
		UPDATE runs SET finished_at = ?, outcome = ?, report = ?, summary = ?, cost_usd = ? WHERE id = ?`,
		nowString(time.Now()), string(outcome), string(report), summary, costUSD, id)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("finish run: %w", domain.ErrNotFound)
	}
	return nil
}

// ListRunningOlderThan returns every run still outcome='running' that
// started before t, oldest first, for the run-timeout sweep.
func (rp *Runs) ListRunningOlderThan(ctx context.Context, t time.Time) ([]domain.Run, error) {
	rows, err := rp.d.QueryContext(ctx, `
		SELECT `+runColumns+` FROM runs WHERE outcome = 'running' AND started_at < ? ORDER BY started_at ASC`,
		nowString(t))
	if err != nil {
		return nil, fmt.Errorf("list running runs older than: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
