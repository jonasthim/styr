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

// Deliveries is the repository for the deliveries table: the log of every
// inbound webhook call a trigger received.
type Deliveries struct{ d *DB }

// NewDeliveries constructs a Deliveries repository.
func NewDeliveries(d *DB) *Deliveries { return &Deliveries{d: d} }

const deliveryColumns = `id, trigger_id, received_at, status, reason, dedupe_key, payload, run_id`

// Create inserts a new delivery row. dl.ID must already be set.
func (dr *Deliveries) Create(ctx context.Context, dl domain.Delivery) error {
	_, err := dr.d.ExecContext(ctx, `
		INSERT INTO deliveries (`+deliveryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		dl.ID, dl.TriggerID, nowString(dl.ReceivedAt), string(dl.Status), dl.Reason, dl.DedupeKey,
		string(dl.Payload), runIDArg(dl.RunID))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create delivery: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create delivery: %w", err)
	}
	return nil
}

func runIDArg(id *string) any {
	if id == nil {
		return nil
	}
	return *id
}

func scanDelivery(row interface{ Scan(dest ...any) error }) (*domain.Delivery, error) {
	var (
		dl         domain.Delivery
		status     string
		payload    string
		receivedAt string
		runID      sql.NullString
	)
	if err := row.Scan(&dl.ID, &dl.TriggerID, &receivedAt, &status, &dl.Reason, &dl.DedupeKey, &payload, &runID); err != nil {
		return nil, err
	}
	dl.Status = domain.DeliveryStatus(status)
	dl.Payload = json.RawMessage(payload)
	dl.ReceivedAt = parseTime(receivedAt)
	dl.RunID = nullString(runID)
	return &dl, nil
}

// Get loads a delivery by id.
func (dr *Deliveries) Get(ctx context.Context, id string) (*domain.Delivery, error) {
	row := dr.d.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE id = ?`, id)
	dl, err := scanDelivery(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get delivery: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get delivery: %w", err)
	}
	return dl, nil
}

// ListByTrigger returns up to limit deliveries for triggerID, newest first
// (matching the deliveries_trigger_idx index).
func (dr *Deliveries) ListByTrigger(ctx context.Context, triggerID string, limit int) ([]domain.Delivery, error) {
	rows, err := dr.d.QueryContext(ctx, `
		SELECT `+deliveryColumns+` FROM deliveries
		WHERE trigger_id = ? ORDER BY received_at DESC LIMIT ?`, triggerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list deliveries by trigger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Delivery
	for rows.Next() {
		dl, err := scanDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		out = append(out, *dl)
	}
	return out, rows.Err()
}

// CountSince counts deliveries for triggerID with the given status received
// at or after since, used by the router's storm cap (count of 'accepted'
// deliveries in the trailing hour).
func (dr *Deliveries) CountSince(ctx context.Context, triggerID string, since time.Time, status domain.DeliveryStatus) (int, error) {
	var n int
	err := dr.d.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM deliveries WHERE trigger_id = ? AND received_at >= ? AND status = ?`,
		triggerID, nowString(since), string(status)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count deliveries since: %w", err)
	}
	return n, nil
}

// LastAcceptedByKey returns the most recent accepted delivery for
// triggerID with the given dedupe key, used for dedupe and cooldown
// checks. ErrNotFound if no accepted delivery with that key exists.
func (dr *Deliveries) LastAcceptedByKey(ctx context.Context, triggerID, dedupeKey string) (*domain.Delivery, error) {
	row := dr.d.QueryRowContext(ctx, `
		SELECT `+deliveryColumns+` FROM deliveries
		WHERE trigger_id = ? AND dedupe_key = ? AND status = 'accepted'
		ORDER BY received_at DESC LIMIT 1`, triggerID, dedupeKey)
	dl, err := scanDelivery(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("last accepted delivery by key: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("last accepted delivery by key: %w", err)
	}
	return dl, nil
}

// SetRun records the run a delivery started (or was replayed into).
func (dr *Deliveries) SetRun(ctx context.Context, id, runID string) error {
	res, err := dr.d.ExecContext(ctx, `UPDATE deliveries SET run_id = ? WHERE id = ?`, runID, id)
	if err != nil {
		return fmt.Errorf("set delivery run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set delivery run: %w", domain.ErrNotFound)
	}
	return nil
}
