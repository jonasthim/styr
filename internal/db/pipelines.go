package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
)

// Pipelines is the repository for the pipelines table.
type Pipelines struct{ d *DB }

// NewPipelines constructs a Pipelines repository.
func NewPipelines(d *DB) *Pipelines { return &Pipelines{d: d} }

const pipelineColumns = `id, owner_user_id, name, workspace_id, yaml, created_at, updated_at`

// Create inserts a new pipeline row. p.ID must already be set.
func (p *Pipelines) Create(ctx context.Context, pl domain.Pipeline) error {
	_, err := p.d.ExecContext(ctx, `
		INSERT INTO pipelines (`+pipelineColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		pl.ID, ownerArg(pl.OwnerID), pl.Name, pl.WorkspaceID, pl.YAML, nowString(pl.CreatedAt), nowString(pl.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create pipeline: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create pipeline: %w", err)
	}
	return nil
}

func scanPipeline(row interface{ Scan(dest ...any) error }) (*domain.Pipeline, error) {
	var (
		pl                   domain.Pipeline
		ownerID              sql.NullString
		createdAt, updatedAt string
	)
	if err := row.Scan(&pl.ID, &ownerID, &pl.Name, &pl.WorkspaceID, &pl.YAML, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	pl.OwnerID = nullString(ownerID)
	pl.CreatedAt = parseTime(createdAt)
	pl.UpdatedAt = parseTime(updatedAt)
	return &pl, nil
}

// Get loads a pipeline by id, with no visibility filtering.
func (p *Pipelines) Get(ctx context.Context, id string) (*domain.Pipeline, error) {
	row := p.d.QueryRowContext(ctx, `SELECT `+pipelineColumns+` FROM pipelines WHERE id = ?`, id)
	pl, err := scanPipeline(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get pipeline: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get pipeline: %w", err)
	}
	return pl, nil
}

// GetByName loads a pipeline by workspace and name, used to resolve a
// trigger's or schedule's pipeline_id reference by the name an operator
// typed. Since names are unique per owner across every workspace (see the
// pipelines_owner_name index), the workspace filter is a defensive
// cross-check rather than the primary key.
func (p *Pipelines) GetByName(ctx context.Context, workspaceID, name string) (*domain.Pipeline, error) {
	row := p.d.QueryRowContext(ctx, `SELECT `+pipelineColumns+` FROM pipelines WHERE workspace_id = ? AND name = ?`, workspaceID, name)
	pl, err := scanPipeline(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get pipeline by name: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get pipeline by name: %w", err)
	}
	return pl, nil
}

// ListVisible returns every pipeline visible to userID: pipelines they
// own, shared (owner-less) pipelines, and — for an admin — every
// pipeline, ordered by name.
func (p *Pipelines) ListVisible(ctx context.Context, userID string, isAdmin bool) ([]domain.Pipeline, error) {
	query := `SELECT ` + pipelineColumns + ` FROM pipelines`
	var args []any
	if !isAdmin {
		query += ` WHERE owner_user_id = ? OR owner_user_id IS NULL`
		args = append(args, userID)
	}
	query += ` ORDER BY name`

	rows, err := p.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list visible pipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Pipeline
	for rows.Next() {
		pl, err := scanPipeline(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pipeline: %w", err)
		}
		out = append(out, *pl)
	}
	return out, rows.Err()
}

// Update replaces a pipeline's mutable fields (everything but id and
// created_at).
func (p *Pipelines) Update(ctx context.Context, pl domain.Pipeline) error {
	res, err := p.d.ExecContext(ctx, `
		UPDATE pipelines SET owner_user_id = ?, name = ?, workspace_id = ?, yaml = ?, updated_at = ?
		WHERE id = ?`,
		ownerArg(pl.OwnerID), pl.Name, pl.WorkspaceID, pl.YAML, nowString(pl.UpdatedAt), pl.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("update pipeline: %w", domain.ErrConflict)
		}
		return fmt.Errorf("update pipeline: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update pipeline: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes a pipeline by id.
func (p *Pipelines) Delete(ctx context.Context, id string) error {
	res, err := p.d.ExecContext(ctx, `DELETE FROM pipelines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete pipeline: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete pipeline: %w", domain.ErrNotFound)
	}
	return nil
}
