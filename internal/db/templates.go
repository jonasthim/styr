package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
)

// Templates is the repository for the templates table.
type Templates struct{ d *DB }

// NewTemplates constructs a Templates repository.
func NewTemplates(d *DB) *Templates { return &Templates{d: d} }

const templateColumns = `id, owner_user_id, name, workspace_id, profile_id, title_template, prompt_template,
	system_prompt, report_schema, created_at, updated_at`

// Create inserts a new template row. t.ID must already be set.
func (t *Templates) Create(ctx context.Context, tpl domain.Template) error {
	_, err := t.d.ExecContext(ctx, `
		INSERT INTO templates (`+templateColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tpl.ID, ownerArg(tpl.OwnerID), tpl.Name, tpl.WorkspaceID, tpl.ProfileID, tpl.TitleTemplate,
		tpl.PromptTemplate, tpl.SystemPrompt, tpl.ReportSchema, nowString(tpl.CreatedAt), nowString(tpl.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create template: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create template: %w", err)
	}
	return nil
}

func scanTemplate(row interface{ Scan(dest ...any) error }) (*domain.Template, error) {
	var (
		tpl                  domain.Template
		ownerID              sql.NullString
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&tpl.ID, &ownerID, &tpl.Name, &tpl.WorkspaceID, &tpl.ProfileID, &tpl.TitleTemplate,
		&tpl.PromptTemplate, &tpl.SystemPrompt, &tpl.ReportSchema, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	tpl.OwnerID = nullString(ownerID)
	tpl.CreatedAt = parseTime(createdAt)
	tpl.UpdatedAt = parseTime(updatedAt)
	return &tpl, nil
}

// Get loads a template by id, with no visibility filtering.
func (t *Templates) Get(ctx context.Context, id string) (*domain.Template, error) {
	row := t.d.QueryRowContext(ctx, `SELECT `+templateColumns+` FROM templates WHERE id = ?`, id)
	tpl, err := scanTemplate(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get template: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get template: %w", err)
	}
	return tpl, nil
}

// ListVisible returns every template visible to userID: templates they
// own, shared (owner-less) templates, and — for an admin — every
// template, ordered by name.
func (t *Templates) ListVisible(ctx context.Context, userID string, isAdmin bool) ([]domain.Template, error) {
	query := `SELECT ` + templateColumns + ` FROM templates`
	var args []any
	if !isAdmin {
		query += ` WHERE owner_user_id = ? OR owner_user_id IS NULL`
		args = append(args, userID)
	}
	query += ` ORDER BY name`

	rows, err := t.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list visible templates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Template
	for rows.Next() {
		tpl, err := scanTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		out = append(out, *tpl)
	}
	return out, rows.Err()
}

// Update replaces a template's mutable fields (everything but id and
// created_at).
func (t *Templates) Update(ctx context.Context, tpl domain.Template) error {
	res, err := t.d.ExecContext(ctx, `
		UPDATE templates SET owner_user_id = ?, name = ?, workspace_id = ?, profile_id = ?, title_template = ?,
			prompt_template = ?, system_prompt = ?, report_schema = ?, updated_at = ?
		WHERE id = ?`,
		ownerArg(tpl.OwnerID), tpl.Name, tpl.WorkspaceID, tpl.ProfileID, tpl.TitleTemplate,
		tpl.PromptTemplate, tpl.SystemPrompt, tpl.ReportSchema, nowString(tpl.UpdatedAt), tpl.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("update template: %w", domain.ErrConflict)
		}
		return fmt.Errorf("update template: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update template: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes a template by id.
func (t *Templates) Delete(ctx context.Context, id string) error {
	res, err := t.d.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete template: %w", domain.ErrNotFound)
	}
	return nil
}
