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

// StepRuns is the repository for the step_runs table.
type StepRuns struct{ d *DB }

// NewStepRuns constructs a StepRuns repository.
func NewStepRuns(d *DB) *StepRuns { return &StepRuns{d: d} }

const stepRunColumns = `id, pipeline_run_id, step_id, index_in_fanout, item, run_id, attempt, state, report,
	started_at, finished_at, worktree`

// Create inserts a new step run row. sr.ID must already be set.
func (s *StepRuns) Create(ctx context.Context, sr domain.StepRun) error {
	_, err := s.d.ExecContext(ctx, `
		INSERT INTO step_runs (`+stepRunColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sr.ID, sr.PipelineRunID, sr.StepID, sr.IndexInFanout, sr.Item, optionalString(sr.RunID), sr.Attempt,
		string(sr.State), reportOrEmpty(sr.Report), optionalTime(sr.StartedAt), optionalTime(sr.FinishedAt), sr.Worktree)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create step run: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create step run: %w", err)
	}
	return nil
}

// reportOrEmpty normalises a nil/empty report payload to the column
// default ”, matching scanStepRun's "empty column, nil RawMessage"
// convention (see runs.go's scanRun for why: an empty-but-non-nil
// RawMessage is not valid JSON and fails to marshal).
func reportOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

func scanStepRun(row interface{ Scan(dest ...any) error }) (*domain.StepRun, error) {
	var (
		sr                    domain.StepRun
		runID                 sql.NullString
		state                 string
		report                string
		startedAt, finishedAt sql.NullString
	)
	if err := row.Scan(&sr.ID, &sr.PipelineRunID, &sr.StepID, &sr.IndexInFanout, &sr.Item, &runID, &sr.Attempt,
		&state, &report, &startedAt, &finishedAt, &sr.Worktree); err != nil {
		return nil, err
	}
	sr.RunID = nullString(runID)
	sr.State = domain.StepRunState(state)
	if report != "" {
		sr.Report = json.RawMessage(report)
	}
	sr.StartedAt = nullTime(startedAt)
	sr.FinishedAt = nullTime(finishedAt)
	return &sr, nil
}

// Get loads a step run by id.
func (s *StepRuns) Get(ctx context.Context, id string) (*domain.StepRun, error) {
	row := s.d.QueryRowContext(ctx, `SELECT `+stepRunColumns+` FROM step_runs WHERE id = ?`, id)
	sr, err := scanStepRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get step run: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get step run: %w", err)
	}
	return sr, nil
}

// ListByPipelineRun returns every step run belonging to pipelineRunID,
// oldest first (matching the step_runs_pipeline_idx index), for the
// pipeline run detail view and the executor's advance().
func (s *StepRuns) ListByPipelineRun(ctx context.Context, pipelineRunID string) ([]domain.StepRun, error) {
	rows, err := s.d.QueryContext(ctx,
		`SELECT `+stepRunColumns+` FROM step_runs WHERE pipeline_run_id = ? ORDER BY rowid ASC`, pipelineRunID)
	if err != nil {
		return nil, fmt.Errorf("list step runs by pipeline run: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.StepRun
	for rows.Next() {
		sr, err := scanStepRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan step run: %w", err)
		}
		out = append(out, *sr)
	}
	return out, rows.Err()
}

// Update replaces a step run's mutable execution fields: state, the run it
// is attached to, its structured report, the worktree it ran in and its
// started/finished timestamps.
func (s *StepRuns) Update(ctx context.Context, id string, state domain.StepRunState, runID *string,
	report json.RawMessage, worktree string, startedAt, finishedAt *time.Time,
) error {
	res, err := s.d.ExecContext(ctx, `
		UPDATE step_runs SET state = ?, run_id = ?, report = ?, worktree = ?, started_at = ?, finished_at = ?
		WHERE id = ?`,
		string(state), optionalString(runID), reportOrEmpty(report), worktree, optionalTime(startedAt), optionalTime(finishedAt), id)
	if err != nil {
		return fmt.Errorf("update step run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update step run: %w", domain.ErrNotFound)
	}
	return nil
}

// GetByRun loads the step run attached to runID, used by the executor to
// map a run.finished/run.failed bus event back to the step-run attempt it
// belongs to.
func (s *StepRuns) GetByRun(ctx context.Context, runID string) (*domain.StepRun, error) {
	row := s.d.QueryRowContext(ctx, `SELECT `+stepRunColumns+` FROM step_runs WHERE run_id = ?`, runID)
	sr, err := scanStepRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get step run by run: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get step run by run: %w", err)
	}
	return sr, nil
}
