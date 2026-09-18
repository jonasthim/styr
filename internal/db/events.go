package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// Events is the repository for the events table: an append-only, per-session
// sequenced log.
type Events struct{ d *DB }

// NewEvents constructs an Events repository.
func NewEvents(d *DB) *Events { return &Events{d: d} }

// Append inserts a new event for sessionID with seq = (max existing seq for
// that session) + 1, computed and inserted inside one transaction so
// concurrent appends never collide.
func (e *Events) Append(ctx context.Context, sessionID string, typ string, payload json.RawMessage) (int64, error) {
	tx, err := e.d.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var maxSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events WHERE session_id = ?`, sessionID).Scan(&maxSeq); err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	seq := maxSeq + 1

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events (session_id, seq, at, type, payload) VALUES (?, ?, ?, ?, ?)`,
		sessionID, seq, nowString(time.Now()), typ, string(payload)); err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	return seq, nil
}

// ListAfter returns up to limit events for sessionID with seq > afterSeq,
// ordered by seq ascending.
func (e *Events) ListAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]domain.Event, error) {
	rows, err := e.d.QueryContext(ctx, `
		SELECT id, session_id, seq, at, type, payload FROM events
		WHERE session_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`,
		sessionID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Event
	for rows.Next() {
		var (
			ev      domain.Event
			at      string
			payload string
		)
		if err := rows.Scan(&ev.ID, &ev.SessionID, &ev.Seq, &at, &ev.Type, &payload); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.At = parseTime(at)
		ev.Payload = json.RawMessage(payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}
