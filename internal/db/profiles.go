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

// Profiles is the repository for the profiles table. Builtin rows (seeded
// by the migration) cannot be deleted, so this repository exposes no
// Delete method.
type Profiles struct{ d *DB }

// NewProfiles constructs a Profiles repository.
func NewProfiles(d *DB) *Profiles { return &Profiles{d: d} }

const profileColumns = `id, name, mode, allowed_tools, disallowed_tools, max_turns, unattended, approval_timeout_s, builtin`

func marshalToolList(tools []string) string {
	if tools == nil {
		tools = []string{}
	}
	b, _ := json.Marshal(tools)
	return string(b)
}

// Create inserts a new profile row. p.ID must already be set.
func (p *Profiles) Create(ctx context.Context, pr domain.Profile) error {
	_, err := p.d.ExecContext(ctx, `
		INSERT INTO profiles (`+profileColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pr.ID, pr.Name, pr.Mode, marshalToolList(pr.AllowedTools), marshalToolList(pr.DisallowedTools),
		pr.MaxTurns, boolToInt(pr.Unattended), int(pr.ApprovalTimeout/time.Second), boolToInt(pr.Builtin))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create profile: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create profile: %w", err)
	}
	return nil
}

func scanProfile(row interface{ Scan(dest ...any) error }) (*domain.Profile, error) {
	var (
		pr                  domain.Profile
		allowed, disallowed string
		unattended, builtin int
		approvalTimeoutS    int
	)
	if err := row.Scan(&pr.ID, &pr.Name, &pr.Mode, &allowed, &disallowed, &pr.MaxTurns,
		&unattended, &approvalTimeoutS, &builtin); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(allowed), &pr.AllowedTools)
	_ = json.Unmarshal([]byte(disallowed), &pr.DisallowedTools)
	pr.Unattended = unattended != 0
	pr.Builtin = builtin != 0
	pr.ApprovalTimeout = time.Duration(approvalTimeoutS) * time.Second
	return &pr, nil
}

// Get loads a profile by id.
func (p *Profiles) Get(ctx context.Context, id string) (*domain.Profile, error) {
	row := p.d.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles WHERE id = ?`, id)
	pr, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get profile: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return pr, nil
}

// List returns every profile ordered by name.
func (p *Profiles) List(ctx context.Context) ([]domain.Profile, error) {
	rows, err := p.d.QueryContext(ctx, `SELECT `+profileColumns+` FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Profile
	for rows.Next() {
		pr, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan profile: %w", err)
		}
		out = append(out, *pr)
	}
	return out, rows.Err()
}

// Update replaces a profile's mutable fields.
func (p *Profiles) Update(ctx context.Context, pr domain.Profile) error {
	res, err := p.d.ExecContext(ctx, `
		UPDATE profiles SET name = ?, mode = ?, allowed_tools = ?, disallowed_tools = ?,
			max_turns = ?, unattended = ?, approval_timeout_s = ? WHERE id = ?`,
		pr.Name, pr.Mode, marshalToolList(pr.AllowedTools), marshalToolList(pr.DisallowedTools),
		pr.MaxTurns, boolToInt(pr.Unattended), int(pr.ApprovalTimeout/time.Second), pr.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("update profile: %w", domain.ErrConflict)
		}
		return fmt.Errorf("update profile: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update profile: %w", domain.ErrNotFound)
	}
	return nil
}
