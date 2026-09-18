package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// LoginSessions is the repository for the login_sessions table: browser
// cookie sessions, identified by the sha256 hash of the bearer token.
type LoginSessions struct{ d *DB }

// NewLoginSessions constructs a LoginSessions repository.
func NewLoginSessions(d *DB) *LoginSessions { return &LoginSessions{d: d} }

// Create inserts a new login session row and returns its generated id.
func (l *LoginSessions) Create(ctx context.Context, userID, tokenHash string, expires time.Time, ua string) (string, error) {
	id := uuid.NewString()
	now := nowString(time.Now())
	_, err := l.d.ExecContext(ctx, `
		INSERT INTO login_sessions (id, user_id, token_hash, created_at, last_seen_at, expires_at, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, tokenHash, now, now, nowString(expires), ua)
	if err != nil {
		if isUniqueViolation(err) {
			return "", fmt.Errorf("create login session: %w", domain.ErrConflict)
		}
		return "", fmt.Errorf("create login session: %w", err)
	}
	return id, nil
}

// GetByHash resolves a login session by its token hash, returning the row's
// own id along with the user id, expiry and last-seen timestamps. Returning
// id lets callers Touch or Delete the exact row without keeping any side
// state of their own (e.g. across a process restart).
func (l *LoginSessions) GetByHash(ctx context.Context, hash string) (id string, userID string, expires time.Time, lastSeen time.Time, err error) {
	var expiresAt, lastSeenAt string
	row := l.d.QueryRowContext(ctx, `SELECT id, user_id, expires_at, last_seen_at FROM login_sessions WHERE token_hash = ?`, hash)
	if err = row.Scan(&id, &userID, &expiresAt, &lastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", time.Time{}, time.Time{}, fmt.Errorf("get login session: %w", domain.ErrNotFound)
		}
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("get login session: %w", err)
	}
	return id, userID, parseTime(expiresAt), parseTime(lastSeenAt), nil
}

// Touch bumps last_seen_at to the current time.
func (l *LoginSessions) Touch(ctx context.Context, id string) error {
	res, err := l.d.ExecContext(ctx, `UPDATE login_sessions SET last_seen_at = ? WHERE id = ?`, nowString(time.Now()), id)
	if err != nil {
		return fmt.Errorf("touch login session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("touch login session: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes a login session (logout).
func (l *LoginSessions) Delete(ctx context.Context, id string) error {
	res, err := l.d.ExecContext(ctx, `DELETE FROM login_sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete login session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete login session: %w", domain.ErrNotFound)
	}
	return nil
}

// DeleteAllForUser removes every login session belonging to a user.
func (l *LoginSessions) DeleteAllForUser(ctx context.Context, userID string) error {
	if _, err := l.d.ExecContext(ctx, `DELETE FROM login_sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete login sessions for user: %w", err)
	}
	return nil
}
