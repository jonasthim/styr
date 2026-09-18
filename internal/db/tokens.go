package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// serviceTokenUserID is the sentinel user_id under which the single
// service-wide Claude token is stored.
const serviceTokenUserID = "__service__"

// Tokens is the repository for the claude_tokens table. Ciphertext and
// nonce are opaque to this package; callers seal/open them with
// internal/crypto before storing and after loading.
type Tokens struct{ d *DB }

// NewTokens constructs a Tokens repository.
func NewTokens(d *DB) *Tokens { return &Tokens{d: d} }

// Set stores (or replaces) the encrypted token for userID.
func (t *Tokens) Set(ctx context.Context, userID string, ciphertext, nonce []byte, label string) error {
	_, err := t.d.ExecContext(ctx, `
		INSERT INTO claude_tokens (user_id, ciphertext, nonce, label, added_at, verified_at)
		VALUES (?, ?, ?, ?, ?, NULL)
		ON CONFLICT (user_id) DO UPDATE SET
			ciphertext = excluded.ciphertext, nonce = excluded.nonce, label = excluded.label,
			added_at = excluded.added_at, verified_at = NULL`,
		userID, ciphertext, nonce, label, nowString(time.Now()))
	if err != nil {
		return fmt.Errorf("set token: %w", err)
	}
	return nil
}

// Get loads the encrypted token for userID.
func (t *Tokens) Get(ctx context.Context, userID string) (ciphertext, nonce []byte, label string, verifiedAt *time.Time, err error) {
	var verified sql.NullString
	row := t.d.QueryRowContext(ctx, `SELECT ciphertext, nonce, label, verified_at FROM claude_tokens WHERE user_id = ?`, userID)
	if err = row.Scan(&ciphertext, &nonce, &label, &verified); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, "", nil, fmt.Errorf("get token: %w", domain.ErrNotFound)
		}
		return nil, nil, "", nil, fmt.Errorf("get token: %w", err)
	}
	return ciphertext, nonce, label, nullTime(verified), nil
}

// MarkVerified stamps verified_at with the current time.
func (t *Tokens) MarkVerified(ctx context.Context, userID string) error {
	res, err := t.d.ExecContext(ctx, `UPDATE claude_tokens SET verified_at = ? WHERE user_id = ?`, nowString(time.Now()), userID)
	if err != nil {
		return fmt.Errorf("mark verified: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("mark verified: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes the stored token for userID.
func (t *Tokens) Delete(ctx context.Context, userID string) error {
	res, err := t.d.ExecContext(ctx, `DELETE FROM claude_tokens WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete token: %w", domain.ErrNotFound)
	}
	return nil
}

// SetService stores the single service-wide Claude token.
func (t *Tokens) SetService(ctx context.Context, ciphertext, nonce []byte, label string) error {
	return t.Set(ctx, serviceTokenUserID, ciphertext, nonce, label)
}

// GetService loads the single service-wide Claude token.
func (t *Tokens) GetService(ctx context.Context) (ciphertext, nonce []byte, label string, verifiedAt *time.Time, err error) {
	return t.Get(ctx, serviceTokenUserID)
}
