package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// Triggers is the repository for the triggers table.
type Triggers struct{ d *DB }

// NewTriggers constructs a Triggers repository.
func NewTriggers(d *DB) *Triggers { return &Triggers{d: d} }

const triggerColumns = `id, owner_user_id, name, slug, kind, secret_hash, secret_hint, template_id, pipeline_id, enabled,
	dedupe_key_template, cooldown_s, storm_cap_per_hour, run_on_resolved, created_at, updated_at, last_delivery_at`

// Create inserts a new trigger row. tr.ID must already be set.
func (t *Triggers) Create(ctx context.Context, tr domain.Trigger) error {
	_, err := t.d.ExecContext(ctx, `
		INSERT INTO triggers (`+triggerColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tr.ID, ownerArg(tr.OwnerID), tr.Name, tr.Slug, string(tr.Kind), tr.SecretHash, tr.SecretHint,
		emptyToNull(tr.TemplateID), optionalString(tr.PipelineID), boolToInt(tr.Enabled), tr.DedupeKeyTemplate, tr.CooldownS, tr.StormCapPerHour,
		boolToInt(tr.RunOnResolved), nowString(tr.CreatedAt), nowString(tr.UpdatedAt), optionalTime(tr.LastDeliveryAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create trigger: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create trigger: %w", err)
	}
	return nil
}

func scanTrigger(row interface{ Scan(dest ...any) error }) (*domain.Trigger, error) {
	var (
		tr                   domain.Trigger
		ownerID              sql.NullString
		kind                 string
		templateID           sql.NullString
		pipelineID           sql.NullString
		enabled, runOnResolv int
		createdAt, updatedAt string
		lastDeliveryAt       sql.NullString
	)
	if err := row.Scan(
		&tr.ID, &ownerID, &tr.Name, &tr.Slug, &kind, &tr.SecretHash, &tr.SecretHint, &templateID, &pipelineID,
		&enabled, &tr.DedupeKeyTemplate, &tr.CooldownS, &tr.StormCapPerHour, &runOnResolv,
		&createdAt, &updatedAt, &lastDeliveryAt,
	); err != nil {
		return nil, err
	}
	tr.OwnerID = nullString(ownerID)
	tr.Kind = domain.TriggerKind(kind)
	// template_id is NULL for a trigger that starts a pipeline instead of a
	// template (migration 00009); the domain uses "" for that.
	tr.TemplateID = templateID.String
	tr.PipelineID = nullString(pipelineID)
	tr.Enabled = enabled != 0
	tr.RunOnResolved = runOnResolv != 0
	tr.CreatedAt = parseTime(createdAt)
	tr.UpdatedAt = parseTime(updatedAt)
	tr.LastDeliveryAt = nullTime(lastDeliveryAt)
	return &tr, nil
}

// Get loads a trigger by id, with no visibility filtering.
func (t *Triggers) Get(ctx context.Context, id string) (*domain.Trigger, error) {
	row := t.d.QueryRowContext(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE id = ?`, id)
	tr, err := scanTrigger(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get trigger: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get trigger: %w", err)
	}
	return tr, nil
}

// GetBySlug loads a trigger by its URL slug, used by the inbound
// `/hooks/{slug}` router.
func (t *Triggers) GetBySlug(ctx context.Context, slug string) (*domain.Trigger, error) {
	row := t.d.QueryRowContext(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE slug = ?`, slug)
	tr, err := scanTrigger(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get trigger by slug: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get trigger by slug: %w", err)
	}
	return tr, nil
}

// ListVisible returns every trigger visible to userID: triggers they own,
// shared (owner-less) triggers, and — for an admin — every trigger,
// ordered by name.
func (t *Triggers) ListVisible(ctx context.Context, userID string, isAdmin bool) ([]domain.Trigger, error) {
	query := `SELECT ` + triggerColumns + ` FROM triggers`
	var args []any
	if !isAdmin {
		query += ` WHERE owner_user_id = ? OR owner_user_id IS NULL`
		args = append(args, userID)
	}
	query += ` ORDER BY name`

	rows, err := t.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list visible triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Trigger
	for rows.Next() {
		tr, err := scanTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("scan trigger: %w", err)
		}
		out = append(out, *tr)
	}
	return out, rows.Err()
}

// Update replaces a trigger's mutable configuration fields. The secret
// (SetSecret) and last_delivery_at (TouchDelivery) are updated separately.
func (t *Triggers) Update(ctx context.Context, tr domain.Trigger) error {
	res, err := t.d.ExecContext(ctx, `
		UPDATE triggers SET owner_user_id = ?, name = ?, kind = ?, template_id = ?, pipeline_id = ?, enabled = ?,
			dedupe_key_template = ?, cooldown_s = ?, storm_cap_per_hour = ?, run_on_resolved = ?, updated_at = ?
		WHERE id = ?`,
		ownerArg(tr.OwnerID), tr.Name, string(tr.Kind), tr.TemplateID, optionalString(tr.PipelineID), boolToInt(tr.Enabled),
		tr.DedupeKeyTemplate, tr.CooldownS, tr.StormCapPerHour, boolToInt(tr.RunOnResolved),
		nowString(tr.UpdatedAt), tr.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("update trigger: %w", domain.ErrConflict)
		}
		return fmt.Errorf("update trigger: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update trigger: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes a trigger by id.
func (t *Triggers) Delete(ctx context.Context, id string) error {
	res, err := t.d.ExecContext(ctx, `DELETE FROM triggers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete trigger: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete trigger: %w", domain.ErrNotFound)
	}
	return nil
}

// SetSecret replaces a trigger's secret hash and display hint, used on
// creation and on `/triggers/{id}/rotate-secret`.
func (t *Triggers) SetSecret(ctx context.Context, id, hash, hint string) error {
	res, err := t.d.ExecContext(ctx, `UPDATE triggers SET secret_hash = ?, secret_hint = ? WHERE id = ?`, hash, hint, id)
	if err != nil {
		return fmt.Errorf("set trigger secret: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set trigger secret: %w", domain.ErrNotFound)
	}
	return nil
}

// TouchDelivery stamps last_delivery_at with at, called by the router
// after every delivery regardless of its outcome.
func (t *Triggers) TouchDelivery(ctx context.Context, id string, at time.Time) error {
	res, err := t.d.ExecContext(ctx, `UPDATE triggers SET last_delivery_at = ? WHERE id = ?`, nowString(at), id)
	if err != nil {
		return fmt.Errorf("touch trigger delivery: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("touch trigger delivery: %w", domain.ErrNotFound)
	}
	return nil
}
