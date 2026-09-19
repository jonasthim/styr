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

// PipelineRuns is the repository for the pipeline_runs table.
type PipelineRuns struct{ d *DB }

// NewPipelineRuns constructs a PipelineRuns repository.
func NewPipelineRuns(d *DB) *PipelineRuns { return &PipelineRuns{d: d} }

const pipelineRunColumns = `id, pipeline_id, origin, origin_ref, input, state, started_at, finished_at, cost_usd`

// defaultPipelineRunListLimit caps List when the caller passes a
// non-positive PipelineRunFilter.Limit.
const defaultPipelineRunListLimit = 100

// Create inserts a new pipeline run row. r.ID must already be set.
func (p *PipelineRuns) Create(ctx context.Context, r domain.PipelineRun) error {
	_, err := p.d.ExecContext(ctx, `
		INSERT INTO pipeline_runs (`+pipelineRunColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.PipelineID, r.Origin, r.OriginRef, inputOrEmpty(r.Input), string(r.State),
		nowString(r.StartedAt), optionalTime(r.FinishedAt), r.CostUSD)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create pipeline run: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create pipeline run: %w", err)
	}
	return nil
}

// inputOrEmpty normalises a nil/empty input payload to the column default
// '{}', matching how templates.Vars merging expects valid JSON.
func inputOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func scanPipelineRun(row interface{ Scan(dest ...any) error }) (*domain.PipelineRun, error) {
	var (
		r          domain.PipelineRun
		input      string
		state      string
		startedAt  string
		finishedAt sql.NullString
	)
	if err := row.Scan(&r.ID, &r.PipelineID, &r.Origin, &r.OriginRef, &input, &state, &startedAt, &finishedAt, &r.CostUSD); err != nil {
		return nil, err
	}
	r.Input = json.RawMessage(input)
	r.State = domain.PipelineRunState(state)
	r.StartedAt = parseTime(startedAt)
	r.FinishedAt = nullTime(finishedAt)
	return &r, nil
}

// Get loads a pipeline run by id.
func (p *PipelineRuns) Get(ctx context.Context, id string) (*domain.PipelineRun, error) {
	row := p.d.QueryRowContext(ctx, `SELECT `+pipelineRunColumns+` FROM pipeline_runs WHERE id = ?`, id)
	r, err := scanPipelineRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get pipeline run: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get pipeline run: %w", err)
	}
	return r, nil
}

// List returns pipeline runs matching filter, newest first.
func (p *PipelineRuns) List(ctx context.Context, filter domain.PipelineRunFilter) ([]domain.PipelineRun, error) {
	query := `SELECT ` + pipelineRunColumns + ` FROM pipeline_runs WHERE 1 = 1`
	var args []any
	if filter.PipelineID != "" {
		query += ` AND pipeline_id = ?`
		args = append(args, filter.PipelineID)
	}
	if filter.State != "" {
		query += ` AND state = ?`
		args = append(args, filter.State)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPipelineRunListLimit
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := p.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pipeline runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.PipelineRun
	for rows.Next() {
		r, err := scanPipelineRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pipeline run: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Finish closes out a pipeline run: sets finished_at to now, its state and
// the accumulated cost (the sum of its step runs, per the plan's executor
// section).
func (p *PipelineRuns) Finish(ctx context.Context, id string, state domain.PipelineRunState, costUSD float64) error {
	res, err := p.d.ExecContext(ctx, `
		UPDATE pipeline_runs SET finished_at = ?, state = ?, cost_usd = ? WHERE id = ?`,
		nowString(time.Now()), string(state), costUSD, id)
	if err != nil {
		return fmt.Errorf("finish pipeline run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("finish pipeline run: %w", domain.ErrNotFound)
	}
	return nil
}

// ListRunningOlderThan returns every pipeline run still state='running'
// that started before t, oldest first, for the pipeline-timeout sweep.
func (p *PipelineRuns) ListRunningOlderThan(ctx context.Context, t time.Time) ([]domain.PipelineRun, error) {
	rows, err := p.d.QueryContext(ctx, `
		SELECT `+pipelineRunColumns+` FROM pipeline_runs WHERE state = 'running' AND started_at < ? ORDER BY started_at ASC`,
		nowString(t))
	if err != nil {
		return nil, fmt.Errorf("list running pipeline runs older than: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.PipelineRun
	for rows.Next() {
		r, err := scanPipelineRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pipeline run: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Reopen puts a finished pipeline run back into state 'running' and clears
// finished_at, for POST /pipeline-runs/{id}/retry-failed. Finish is the
// wrong tool for it: it stamps finished_at on a run that has not finished,
// which every "how long has this been going" reader then has to second
// guess. Cost is left alone — the attempts already paid for still count.
func (p *PipelineRuns) Reopen(ctx context.Context, id string) error {
	res, err := p.d.ExecContext(ctx, `
		UPDATE pipeline_runs SET state = 'running', finished_at = NULL WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("reopen pipeline run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("reopen pipeline run: %w", domain.ErrNotFound)
	}
	return nil
}
