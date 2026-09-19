package db

import (
	"context"
	"fmt"
)

// WorkspaceAccess is the repository for the workspace_access table: the
// per-user allowlist for a shared workspace whose access mode is "listed"
// (internal/domain.WorkspaceAccessListed). A workspace whose access mode is
// "everyone" ignores this table entirely — callers check the workspace's
// own access column first and only consult this repository once it reads
// "listed".
type WorkspaceAccess struct{ d *DB }

// NewWorkspaceAccess constructs a WorkspaceAccess repository.
func NewWorkspaceAccess(d *DB) *WorkspaceAccess { return &WorkspaceAccess{d: d} }

// Set replaces the full allowlist for workspaceID with userIDs, in one
// transaction (delete then insert) so a concurrent List/HasAccess never
// observes a partially-replaced list. An empty/nil userIDs clears the list.
func (a *WorkspaceAccess) Set(ctx context.Context, workspaceID string, userIDs []string) error {
	tx, err := a.d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set workspace access: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM workspace_access WHERE workspace_id = ?`, workspaceID); err != nil {
		return fmt.Errorf("set workspace access: %w", err)
	}
	for _, userID := range userIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_access (workspace_id, user_id) VALUES (?, ?)`, workspaceID, userID,
		); err != nil {
			return fmt.Errorf("set workspace access: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set workspace access: %w", err)
	}
	return nil
}

// List returns the ids of the users with explicit access to workspaceID,
// ordered by user id.
func (a *WorkspaceAccess) List(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := a.d.QueryContext(ctx,
		`SELECT user_id FROM workspace_access WHERE workspace_id = ? ORDER BY user_id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace access: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]string, 0)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("scan workspace access: %w", err)
		}
		out = append(out, userID)
	}
	return out, rows.Err()
}

// HasAccess reports whether userID is on workspaceID's allowlist.
func (a *WorkspaceAccess) HasAccess(ctx context.Context, workspaceID, userID string) (bool, error) {
	var n int
	if err := a.d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_access WHERE workspace_id = ? AND user_id = ?`, workspaceID, userID,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("check workspace access: %w", err)
	}
	return n > 0, nil
}
