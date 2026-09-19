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

// Schedules is the repository for the schedules table.
type Schedules struct{ d *DB }

// NewSchedules constructs a Schedules repository.
func NewSchedules(d *DB) *Schedules { return &Schedules{d: d} }

const scheduleColumns = `id, owner_user_id, name, template_id, pipeline_id, cron, enabled, vars,
	last_run_at, last_outcome, next_run_at, created_at, updated_at`

// Create inserts a new schedule row. sc.ID must already be set.
func (s *Schedules) Create(ctx context.Context, sc domain.Schedule) error {
	_, err := s.d.ExecContext(ctx, `
		INSERT INTO schedules (`+scheduleColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ID, ownerArg(sc.OwnerID), sc.Name, emptyToNull(sc.TemplateID), optionalString(sc.PipelineID), sc.Cron, boolToInt(sc.Enabled), varsOrEmpty(sc.Vars),
		optionalTime(sc.LastRunAt), sc.LastOutcome, optionalTime(sc.NextRunAt), nowString(sc.CreatedAt), nowString(sc.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create schedule: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create schedule: %w", err)
	}
	return nil
}

// varsOrEmpty normalises a nil/empty vars payload to the column default
// '{}', matching how templates.Vars merging expects valid JSON.
func varsOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func scanSchedule(row interface{ Scan(dest ...any) error }) (*domain.Schedule, error) {
	var (
		sc                   domain.Schedule
		ownerID              sql.NullString
		templateID           sql.NullString
		pipelineID           sql.NullString
		enabled              int
		vars                 string
		lastRunAt            sql.NullString
		nextRunAt            sql.NullString
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&sc.ID, &ownerID, &sc.Name, &templateID, &pipelineID, &sc.Cron, &enabled, &vars,
		&lastRunAt, &sc.LastOutcome, &nextRunAt, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	sc.OwnerID = nullString(ownerID)
	// template_id is NULL for a schedule that starts a pipeline instead of a
	// template (migration 00009); the domain uses "" for that.
	sc.TemplateID = templateID.String
	sc.PipelineID = nullString(pipelineID)
	sc.Enabled = enabled != 0
	sc.Vars = json.RawMessage(vars)
	sc.LastRunAt = nullTime(lastRunAt)
	sc.NextRunAt = nullTime(nextRunAt)
	sc.CreatedAt = parseTime(createdAt)
	sc.UpdatedAt = parseTime(updatedAt)
	return &sc, nil
}

// Get loads a schedule by id, with no visibility filtering.
func (s *Schedules) Get(ctx context.Context, id string) (*domain.Schedule, error) {
	row := s.d.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules WHERE id = ?`, id)
	sc, err := scanSchedule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get schedule: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	return sc, nil
}

// ListVisible returns every schedule visible to userID: schedules they
// own, shared (owner-less) schedules, and — for an admin — every
// schedule, ordered by name.
func (s *Schedules) ListVisible(ctx context.Context, userID string, isAdmin bool) ([]domain.Schedule, error) {
	query := `SELECT ` + scheduleColumns + ` FROM schedules`
	var args []any
	if !isAdmin {
		query += ` WHERE owner_user_id = ? OR owner_user_id IS NULL`
		args = append(args, userID)
	}
	query += ` ORDER BY name`

	rows, err := s.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list visible schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		out = append(out, *sc)
	}
	return out, rows.Err()
}

// ListDue returns every enabled schedule whose next_run_at is set and at
// or before now, ordered by next_run_at, for the scheduler tick.
func (s *Schedules) ListDue(ctx context.Context, now time.Time) ([]domain.Schedule, error) {
	rows, err := s.d.QueryContext(ctx, `
		SELECT `+scheduleColumns+` FROM schedules
		WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		ORDER BY next_run_at ASC`, nowString(now))
	if err != nil {
		return nil, fmt.Errorf("list due schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		out = append(out, *sc)
	}
	return out, rows.Err()
}

// Update replaces a schedule's mutable configuration fields.
// last_run_at/last_outcome (RecordRun) and next_run_at (SetNextRun) are
// updated separately.
func (s *Schedules) Update(ctx context.Context, sc domain.Schedule) error {
	res, err := s.d.ExecContext(ctx, `
		UPDATE schedules SET owner_user_id = ?, name = ?, template_id = ?, pipeline_id = ?, cron = ?, enabled = ?, vars = ?, updated_at = ?
		WHERE id = ?`,
		ownerArg(sc.OwnerID), sc.Name, emptyToNull(sc.TemplateID), optionalString(sc.PipelineID), sc.Cron, boolToInt(sc.Enabled), varsOrEmpty(sc.Vars),
		nowString(sc.UpdatedAt), sc.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("update schedule: %w", domain.ErrConflict)
		}
		return fmt.Errorf("update schedule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update schedule: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes a schedule by id.
func (s *Schedules) Delete(ctx context.Context, id string) error {
	res, err := s.d.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete schedule: %w", domain.ErrNotFound)
	}
	return nil
}

// SetNextRun updates a schedule's next_run_at, called after every tick
// (whether it started, skipped or failed) and after a cron change. A nil
// next stops the schedule from ever being picked up by ListDue again.
func (s *Schedules) SetNextRun(ctx context.Context, id string, next *time.Time) error {
	res, err := s.d.ExecContext(ctx, `UPDATE schedules SET next_run_at = ? WHERE id = ?`, optionalTime(next), id)
	if err != nil {
		return fmt.Errorf("set schedule next run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set schedule next run: %w", domain.ErrNotFound)
	}
	return nil
}

// RecordRun stamps last_run_at and last_outcome, called after every tick
// (or an explicit "run now") that attempted to start a run.
func (s *Schedules) RecordRun(ctx context.Context, id string, at time.Time, outcome string) error {
	res, err := s.d.ExecContext(ctx, `UPDATE schedules SET last_run_at = ?, last_outcome = ? WHERE id = ?`,
		nowString(at), outcome, id)
	if err != nil {
		return fmt.Errorf("record schedule run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("record schedule run: %w", domain.ErrNotFound)
	}
	return nil
}
