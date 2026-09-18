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

// Users is the repository for the users table.
type Users struct{ d *DB }

// NewUsers constructs a Users repository.
func NewUsers(d *DB) *Users { return &Users{d: d} }

const userColumns = `id, issuer, subject, email, display_name, avatar_url, role, created_at, last_login_at, prefs`

// Create inserts a new user row. u.ID must already be set (callers generate
// it, typically with uuid.NewString).
func (u *Users) Create(ctx context.Context, usr domain.User) error {
	prefs := usr.Prefs
	if len(prefs) == 0 {
		prefs = json.RawMessage(`{}`)
	}
	_, err := u.d.ExecContext(ctx, `
		INSERT INTO users (`+userColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		usr.ID, usr.Issuer, usr.Subject, usr.Email, usr.DisplayName, usr.AvatarURL, string(usr.Role),
		nowString(usr.CreatedAt), nowString(usr.LastLoginAt), string(prefs))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create user: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func scanUser(row interface{ Scan(dest ...any) error }) (*domain.User, error) {
	var (
		usr                  domain.User
		role                 string
		createdAt, lastLogin string
		prefs                string
	)
	if err := row.Scan(&usr.ID, &usr.Issuer, &usr.Subject, &usr.Email, &usr.DisplayName, &usr.AvatarURL,
		&role, &createdAt, &lastLogin, &prefs); err != nil {
		return nil, err
	}
	usr.Role = domain.Role(role)
	usr.CreatedAt = parseTime(createdAt)
	usr.LastLoginAt = parseTime(lastLogin)
	usr.Prefs = json.RawMessage(prefs)
	return &usr, nil
}

// GetByID loads a user by id.
func (u *Users) GetByID(ctx context.Context, id string) (*domain.User, error) {
	row := u.d.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	usr, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get user: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return usr, nil
}

// GetBySubject loads a user by (issuer, subject).
func (u *Users) GetBySubject(ctx context.Context, issuer, subject string) (*domain.User, error) {
	row := u.d.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE issuer = ? AND subject = ?`, issuer, subject)
	usr, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get user by subject: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get user by subject: %w", err)
	}
	return usr, nil
}

// List returns every user ordered by created_at.
func (u *Users) List(ctx context.Context) ([]domain.User, error) {
	rows, err := u.d.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.User
	for rows.Next() {
		usr, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, *usr)
	}
	return out, rows.Err()
}

// UpdateRole changes a user's role.
func (u *Users) UpdateRole(ctx context.Context, id string, role domain.Role) error {
	res, err := u.d.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, string(role), id)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update role: %w", domain.ErrNotFound)
	}
	return nil
}

// TouchLogin stamps last_login_at with the current time.
func (u *Users) TouchLogin(ctx context.Context, id string) error {
	res, err := u.d.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, nowString(time.Now()), id)
	if err != nil {
		return fmt.Errorf("touch login: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("touch login: %w", domain.ErrNotFound)
	}
	return nil
}

// UpdatePrefs replaces the stored prefs JSON blob.
func (u *Users) UpdatePrefs(ctx context.Context, id string, prefs json.RawMessage) error {
	res, err := u.d.ExecContext(ctx, `UPDATE users SET prefs = ? WHERE id = ?`, string(prefs), id)
	if err != nil {
		return fmt.Errorf("update prefs: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update prefs: %w", domain.ErrNotFound)
	}
	return nil
}

// Count returns the total number of users.
func (u *Users) Count(ctx context.Context) (int, error) {
	var n int
	if err := u.d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}
