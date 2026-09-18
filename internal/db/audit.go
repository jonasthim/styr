package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// Audit is the repository for the audit table: a server-wide append-only
// log of actions taken through the UI or API.
type Audit struct{ d *DB }

// NewAudit constructs an Audit repository.
func NewAudit(d *DB) *Audit { return &Audit{d: d} }

// Append inserts a new audit entry. detail is marshalled to JSON; pass nil
// for no detail.
func (a *Audit) Append(ctx context.Context, actor, action, target string, detail any) error {
	payload := []byte(`{}`)
	if detail != nil {
		var err error
		payload, err = json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("marshal audit detail: %w", err)
		}
	}
	_, err := a.d.ExecContext(ctx, `
		INSERT INTO audit (at, actor, action, target, detail) VALUES (?, ?, ?, ?, ?)`,
		nowString(time.Now()), actor, action, target, string(payload))
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

// List returns the most recent audit entries, newest first, up to limit.
func (a *Audit) List(ctx context.Context, limit int) ([]domain.AuditEntry, error) {
	rows, err := a.d.QueryContext(ctx, `SELECT id, at, actor, action, target, detail FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AuditEntry
	for rows.Next() {
		var (
			e      domain.AuditEntry
			at     string
			detail string
		)
		if err := rows.Scan(&e.ID, &at, &e.Actor, &e.Action, &e.Target, &detail); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		e.At = parseTime(at)
		e.Detail = json.RawMessage(detail)
		out = append(out, e)
	}
	return out, rows.Err()
}
