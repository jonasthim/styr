package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// CodexCredentials is the repository for the codex_credentials table: one
// sealed OpenAI API key per user, plus the single service-wide key stored
// under the serviceTokenUserID sentinel (the same convention claude_tokens
// uses for its service token). Ciphertext and nonce are opaque here; callers
// seal and open them with internal/crypto.
//
// Unlike claude_tokens there is no verified_at column: an API key is
// verified before it is stored (the handler refuses to write an unverified
// one) and there is no second, later verification to record.
type CodexCredentials struct{ d *DB }

// NewCodexCredentials constructs a CodexCredentials repository.
func NewCodexCredentials(d *DB) *CodexCredentials { return &CodexCredentials{d: d} }

// Set stores (or replaces) the encrypted OpenAI API key for userID.
func (c *CodexCredentials) Set(ctx context.Context, userID string, ciphertext, nonce []byte, label string) error {
	_, err := c.d.ExecContext(ctx, `
		INSERT INTO codex_credentials (user_id, api_key_ciphertext, nonce, label, added_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET
			api_key_ciphertext = excluded.api_key_ciphertext, nonce = excluded.nonce,
			label = excluded.label, added_at = excluded.added_at`,
		userID, ciphertext, nonce, label, nowString(time.Now()))
	if err != nil {
		return fmt.Errorf("set codex credential: %w", err)
	}
	return nil
}

// Get loads the encrypted OpenAI API key for userID.
func (c *CodexCredentials) Get(ctx context.Context, userID string) (ciphertext, nonce []byte, label string, addedAt time.Time, err error) {
	var added string
	row := c.d.QueryRowContext(ctx, `SELECT api_key_ciphertext, nonce, label, added_at FROM codex_credentials WHERE user_id = ?`, userID)
	if err = row.Scan(&ciphertext, &nonce, &label, &added); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, "", time.Time{}, fmt.Errorf("get codex credential: %w", domain.ErrNotFound)
		}
		return nil, nil, "", time.Time{}, fmt.Errorf("get codex credential: %w", err)
	}
	at, _ := time.Parse(time.RFC3339Nano, added)
	return ciphertext, nonce, label, at, nil
}

// Delete removes the stored key for userID.
func (c *CodexCredentials) Delete(ctx context.Context, userID string) error {
	res, err := c.d.ExecContext(ctx, `DELETE FROM codex_credentials WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete codex credential: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete codex credential: %w", domain.ErrNotFound)
	}
	return nil
}

// SetService stores the single service-wide OpenAI API key.
func (c *CodexCredentials) SetService(ctx context.Context, ciphertext, nonce []byte, label string) error {
	return c.Set(ctx, serviceTokenUserID, ciphertext, nonce, label)
}

// GetService loads the single service-wide OpenAI API key.
func (c *CodexCredentials) GetService(ctx context.Context) (ciphertext, nonce []byte, label string, addedAt time.Time, err error) {
	return c.Get(ctx, serviceTokenUserID)
}
