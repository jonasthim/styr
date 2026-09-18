package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// APITokens is the repository for the api_tokens table: personal access
// tokens (`styr_pat_…`) usable as a bearer credential instead of a cookie
// session.
type APITokens struct{ d *DB }

// NewAPITokens constructs an APITokens repository.
func NewAPITokens(d *DB) *APITokens { return &APITokens{d: d} }

const apiTokenColumns = `id, user_id, name, token_hash, prefix, created_at, last_used_at, expires_at`

// Create inserts a new API token row. t.ID must already be set. Only
// t.TokenHash (sha256 of the plaintext token) is stored — the plaintext is
// never persisted.
func (a *APITokens) Create(ctx context.Context, t domain.APIToken) error {
	_, err := a.d.ExecContext(ctx, `
		INSERT INTO api_tokens (`+apiTokenColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Name, t.TokenHash, t.Prefix, nowString(t.CreatedAt),
		optionalTime(t.LastUsedAt), optionalTime(t.ExpiresAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create api token: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create api token: %w", err)
	}
	return nil
}

func scanAPIToken(row interface{ Scan(dest ...any) error }) (*domain.APIToken, error) {
	var (
		t                     domain.APIToken
		createdAt             string
		lastUsedAt, expiresAt sql.NullString
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.Prefix, &createdAt, &lastUsedAt, &expiresAt); err != nil {
		return nil, err
	}
	t.CreatedAt = parseTime(createdAt)
	t.LastUsedAt = nullTime(lastUsedAt)
	t.ExpiresAt = nullTime(expiresAt)
	return &t, nil
}

// GetByHash loads an API token by the sha256 hash of its plaintext,
// computed by the caller (internal/auth). ErrNotFound for an unknown hash.
func (a *APITokens) GetByHash(ctx context.Context, hash string) (*domain.APIToken, error) {
	row := a.d.QueryRowContext(ctx, `SELECT `+apiTokenColumns+` FROM api_tokens WHERE token_hash = ?`, hash)
	t, err := scanAPIToken(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get api token by hash: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get api token by hash: %w", err)
	}
	return t, nil
}

// ListByUser returns every API token belonging to userID, newest first.
func (a *APITokens) ListByUser(ctx context.Context, userID string) ([]domain.APIToken, error) {
	rows, err := a.d.QueryContext(ctx, `
		SELECT `+apiTokenColumns+` FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens by user: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan api token: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// TouchUsed stamps last_used_at with the current time.
func (a *APITokens) TouchUsed(ctx context.Context, id string, at time.Time) error {
	res, err := a.d.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, nowString(at), id)
	if err != nil {
		return fmt.Errorf("touch api token used: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("touch api token used: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes an API token by id, scoped to userID so one user can
// never revoke another's token.
func (a *APITokens) Delete(ctx context.Context, id, userID string) error {
	res, err := a.d.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete api token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete api token: %w", domain.ErrNotFound)
	}
	return nil
}
