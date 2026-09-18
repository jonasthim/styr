package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
)

// ScheduleFirings is the repository for the schedule_firings table: the
// log of every scheduler tick decision for a schedule.
type ScheduleFirings struct{ d *DB }

// NewScheduleFirings constructs a ScheduleFirings repository.
func NewScheduleFirings(d *DB) *ScheduleFirings { return &ScheduleFirings{d: d} }

const scheduleFiringColumns = `id, schedule_id, fired_at, status, reason, run_id`

// Create inserts a new schedule firing row. f.ID must already be set.
func (f *ScheduleFirings) Create(ctx context.Context, sf domain.ScheduleFiring) error {
	_, err := f.d.ExecContext(ctx, `
		INSERT INTO schedule_firings (`+scheduleFiringColumns+`)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sf.ID, sf.ScheduleID, nowString(sf.FiredAt), string(sf.Status), sf.Reason, runIDArg(sf.RunID))
	if err != nil {
		return fmt.Errorf("create schedule firing: %w", err)
	}
	return nil
}

func scanScheduleFiring(row interface{ Scan(dest ...any) error }) (*domain.ScheduleFiring, error) {
	var (
		sf      domain.ScheduleFiring
		status  string
		firedAt string
		runID   sql.NullString
	)
	if err := row.Scan(&sf.ID, &sf.ScheduleID, &firedAt, &status, &sf.Reason, &runID); err != nil {
		return nil, err
	}
	sf.Status = domain.ScheduleFiringStatus(status)
	sf.FiredAt = parseTime(firedAt)
	sf.RunID = nullString(runID)
	return &sf, nil
}

// ListBySchedule returns up to limit firings for scheduleID, newest first
// (matching the schedule_firings_schedule_idx index).
func (f *ScheduleFirings) ListBySchedule(ctx context.Context, scheduleID string, limit int) ([]domain.ScheduleFiring, error) {
	rows, err := f.d.QueryContext(ctx, `
		SELECT `+scheduleFiringColumns+` FROM schedule_firings
		WHERE schedule_id = ? ORDER BY fired_at DESC LIMIT ?`, scheduleID, limit)
	if err != nil {
		return nil, fmt.Errorf("list schedule firings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ScheduleFiring
	for rows.Next() {
		sf, err := scanScheduleFiring(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule firing: %w", err)
		}
		out = append(out, *sf)
	}
	return out, rows.Err()
}

// LastStartedRunID returns the run_id of the most recent 'started' firing
// for scheduleID, used to check whether a schedule's previous run is still
// going. domain.ErrNotFound when the schedule has never started a run.
func (f *ScheduleFirings) LastStartedRunID(ctx context.Context, scheduleID string) (string, error) {
	var runID sql.NullString
	err := f.d.QueryRowContext(ctx, `
		SELECT run_id FROM schedule_firings
		WHERE schedule_id = ? AND status = 'started'
		ORDER BY fired_at DESC LIMIT 1`, scheduleID).Scan(&runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("last started run id: %w", domain.ErrNotFound)
		}
		return "", fmt.Errorf("last started run id: %w", err)
	}
	if !runID.Valid || runID.String == "" {
		return "", fmt.Errorf("last started run id: %w", domain.ErrNotFound)
	}
	return runID.String, nil
}
