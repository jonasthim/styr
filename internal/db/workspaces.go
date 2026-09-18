package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
)

// Workspaces is the repository for the workspaces table.
type Workspaces struct{ d *DB }

// NewWorkspaces constructs a Workspaces repository.
func NewWorkspaces(d *DB) *Workspaces { return &Workspaces{d: d} }

const workspaceColumns = `id, name, path, default_profile_id, worktrees, created_at`

// Create inserts a new workspace row. w.ID must already be set.
func (w *Workspaces) Create(ctx context.Context, ws domain.Workspace) error {
	_, err := w.d.ExecContext(ctx, `
		INSERT INTO workspaces (`+workspaceColumns+`)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ws.ID, ws.Name, ws.Path, ws.DefaultProfileID, boolToInt(ws.Worktrees), nowString(ws.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create workspace: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create workspace: %w", err)
	}
	return nil
}

func scanWorkspace(row interface{ Scan(dest ...any) error }) (*domain.Workspace, error) {
	var (
		ws        domain.Workspace
		worktrees int
		createdAt string
	)
	if err := row.Scan(&ws.ID, &ws.Name, &ws.Path, &ws.DefaultProfileID, &worktrees, &createdAt); err != nil {
		return nil, err
	}
	ws.Worktrees = worktrees != 0
	ws.CreatedAt = parseTime(createdAt)
	return &ws, nil
}

// Get loads a workspace by id.
func (w *Workspaces) Get(ctx context.Context, id string) (*domain.Workspace, error) {
	row := w.d.QueryRowContext(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id = ?`, id)
	ws, err := scanWorkspace(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get workspace: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return ws, nil
}

// List returns every workspace ordered by name.
func (w *Workspaces) List(ctx context.Context) ([]domain.Workspace, error) {
	rows, err := w.d.QueryContext(ctx, `SELECT `+workspaceColumns+` FROM workspaces ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Workspace
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		out = append(out, *ws)
	}
	return out, rows.Err()
}

// Update replaces a workspace's mutable fields.
func (w *Workspaces) Update(ctx context.Context, ws domain.Workspace) error {
	res, err := w.d.ExecContext(ctx, `
		UPDATE workspaces SET name = ?, path = ?, default_profile_id = ?, worktrees = ? WHERE id = ?`,
		ws.Name, ws.Path, ws.DefaultProfileID, boolToInt(ws.Worktrees), ws.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("update workspace: %w", domain.ErrConflict)
		}
		return fmt.Errorf("update workspace: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update workspace: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes a workspace by id.
func (w *Workspaces) Delete(ctx context.Context, id string) error {
	res, err := w.d.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete workspace: %w", domain.ErrNotFound)
	}
	return nil
}
