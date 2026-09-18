package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// Workspaces is the repository for the workspaces table.
type Workspaces struct{ d *DB }

// NewWorkspaces constructs a Workspaces repository.
func NewWorkspaces(d *DB) *Workspaces { return &Workspaces{d: d} }

const workspaceColumns = `id, owner_user_id, name, path, default_profile_id, worktrees, base_branch, auto_checkpoint,
	source, repo_url, branch, managed, state, error, created_at, updated_at`

// Create inserts a new workspace row. w.ID must already be set.
func (w *Workspaces) Create(ctx context.Context, ws domain.Workspace) error {
	_, err := w.d.ExecContext(ctx, `
		INSERT INTO workspaces (`+workspaceColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ws.ID, ownerArg(ws.OwnerID), ws.Name, ws.Path, ws.DefaultProfileID, boolToInt(ws.Worktrees),
		ws.BaseBranch, boolToInt(ws.AutoCheckpoint),
		string(ws.Source), ws.RepoURL, ws.Branch, boolToInt(ws.Managed), string(ws.State), ws.Error,
		nowString(ws.CreatedAt), nowString(ws.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create workspace: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create workspace: %w", err)
	}
	return nil
}

// ownerArg converts a nullable owner id into a driver-friendly value.
func ownerArg(ownerID *string) any {
	if ownerID == nil {
		return nil
	}
	return *ownerID
}

func scanWorkspace(row interface{ Scan(dest ...any) error }) (*domain.Workspace, error) {
	var (
		ws                   domain.Workspace
		ownerID              sql.NullString
		worktrees, managed   int
		autoCheckpoint       int
		source, state        string
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&ws.ID, &ownerID, &ws.Name, &ws.Path, &ws.DefaultProfileID, &worktrees, &ws.BaseBranch, &autoCheckpoint,
		&source, &ws.RepoURL, &ws.Branch, &managed, &state, &ws.Error, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	ws.OwnerID = nullString(ownerID)
	ws.Worktrees = worktrees != 0
	ws.AutoCheckpoint = autoCheckpoint != 0
	ws.Managed = managed != 0
	ws.Source = domain.WorkspaceSource(source)
	ws.State = domain.WorkspaceState(state)
	ws.CreatedAt = parseTime(createdAt)
	ws.UpdatedAt = parseTime(updatedAt)
	return &ws, nil
}

// Get loads a workspace by id, with no visibility filtering. Callers that
// must enforce owner/admin visibility do so themselves (see
// internal/workspaces.Service and internal/sessions.Service).
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

// ListVisible returns every workspace visible to userID: workspaces they
// own, shared (owner-less) workspaces, and — for an admin — every
// workspace, ordered by name.
func (w *Workspaces) ListVisible(ctx context.Context, userID string, isAdmin bool) ([]domain.Workspace, error) {
	query := `SELECT ` + workspaceColumns + ` FROM workspaces`
	var args []any
	if !isAdmin {
		query += ` WHERE owner_user_id = ? OR owner_user_id IS NULL`
		args = append(args, userID)
	}
	query += ` ORDER BY name`

	rows, err := w.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list visible workspaces: %w", err)
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

// Update replaces a workspace's mutable fields (everything but id and
// created_at).
func (w *Workspaces) Update(ctx context.Context, ws domain.Workspace) error {
	res, err := w.d.ExecContext(ctx, `
		UPDATE workspaces SET owner_user_id = ?, name = ?, path = ?, default_profile_id = ?, worktrees = ?,
			base_branch = ?, auto_checkpoint = ?,
			source = ?, repo_url = ?, branch = ?, managed = ?, state = ?, error = ?, updated_at = ?
		WHERE id = ?`,
		ownerArg(ws.OwnerID), ws.Name, ws.Path, ws.DefaultProfileID, boolToInt(ws.Worktrees),
		ws.BaseBranch, boolToInt(ws.AutoCheckpoint),
		string(ws.Source), ws.RepoURL, ws.Branch, boolToInt(ws.Managed), string(ws.State), ws.Error,
		nowString(ws.UpdatedAt), ws.ID)
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

// SetState updates only a workspace's state, error message and updated_at,
// for the async clone lifecycle where the caller does not hold (and must
// not clobber) the rest of the row.
func (w *Workspaces) SetState(ctx context.Context, id string, state domain.WorkspaceState, errMsg string, updatedAt time.Time) error {
	res, err := w.d.ExecContext(ctx, `UPDATE workspaces SET state = ?, error = ?, updated_at = ? WHERE id = ?`,
		string(state), errMsg, nowString(updatedAt), id)
	if err != nil {
		return fmt.Errorf("set workspace state: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set workspace state: %w", domain.ErrNotFound)
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
