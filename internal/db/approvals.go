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

// Approvals is the repository for the approvals table.
type Approvals struct{ d *DB }

// NewApprovals constructs an Approvals repository.
func NewApprovals(d *DB) *Approvals { return &Approvals{d: d} }

const approvalColumns = `id, session_id, request_id, tool, input, risk, state, created_at,
	decided_by, decided_at, snoozed_until, updated_input, message`

// Create inserts a new approval row. a.ID must already be set.
func (a *Approvals) Create(ctx context.Context, ap domain.Approval) error {
	_, err := a.d.ExecContext(ctx, `
		INSERT INTO approvals (`+approvalColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ap.ID, ap.SessionID, ap.RequestID, ap.Tool, string(ap.Input), string(ap.Risk), string(ap.State),
		nowString(ap.CreatedAt), ap.DecidedBy, optionalTime(ap.DecidedAt), optionalTime(ap.SnoozedUntil),
		optionalJSON(ap.UpdatedInput), ap.Message)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create approval: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create approval: %w", err)
	}
	return nil
}

func optionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return nowString(*t)
}

func optionalJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func scanApproval(row interface{ Scan(dest ...any) error }) (*domain.Approval, error) {
	var (
		ap                      domain.Approval
		risk, state             string
		input                   string
		createdAt               string
		decidedBy               sql.NullString
		decidedAt, snoozedUntil sql.NullString
		updatedInput            sql.NullString
	)
	if err := row.Scan(&ap.ID, &ap.SessionID, &ap.RequestID, &ap.Tool, &input, &risk, &state, &createdAt,
		&decidedBy, &decidedAt, &snoozedUntil, &updatedInput, &ap.Message); err != nil {
		return nil, err
	}
	ap.Input = json.RawMessage(input)
	ap.Risk = domain.RiskTier(risk)
	ap.State = domain.ApprovalState(state)
	ap.CreatedAt = parseTime(createdAt)
	ap.DecidedBy = nullString(decidedBy)
	ap.DecidedAt = nullTime(decidedAt)
	ap.SnoozedUntil = nullTime(snoozedUntil)
	if updatedInput.Valid && updatedInput.String != "" {
		ap.UpdatedInput = json.RawMessage(updatedInput.String)
	}
	return &ap, nil
}

// Get loads an approval by id.
func (a *Approvals) Get(ctx context.Context, id string) (*domain.Approval, error) {
	row := a.d.QueryRowContext(ctx, `SELECT `+approvalColumns+` FROM approvals WHERE id = ?`, id)
	ap, err := scanApproval(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get approval: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get approval: %w", err)
	}
	return ap, nil
}

// approvalColumnsQualified is approvalColumns qualified with the "a" alias,
// for the ListPendingVisible query that joins sessions.
const approvalColumnsQualified = `a.id, a.session_id, a.request_id, a.tool, a.input, a.risk, a.state, a.created_at,
	a.decided_by, a.decided_at, a.snoozed_until, a.updated_input, a.message`

// ListPendingVisible returns pending approvals whose session is visible to
// userID: owned by them, owner-less, or every session when isAdmin.
func (a *Approvals) ListPendingVisible(ctx context.Context, userID string, isAdmin bool) ([]domain.Approval, error) {
	query := `SELECT ` + approvalColumnsQualified + `
		FROM approvals a JOIN sessions s ON s.id = a.session_id
		WHERE a.state = 'pending'`
	args := []any{}
	if !isAdmin {
		query += ` AND (s.owner_user_id = ? OR s.owner_user_id IS NULL)`
		args = append(args, userID)
	}
	query += ` ORDER BY a.created_at`

	return a.queryList(ctx, query, args...)
}

// PendingForSession returns pending approvals for one session.
func (a *Approvals) PendingForSession(ctx context.Context, sessionID string) ([]domain.Approval, error) {
	query := `SELECT ` + approvalColumns + ` FROM approvals WHERE session_id = ? AND state = 'pending' ORDER BY created_at`
	return a.queryList(ctx, query, sessionID)
}

func (a *Approvals) queryList(ctx context.Context, query string, args ...any) ([]domain.Approval, error) {
	rows, err := a.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Approval
	for rows.Next() {
		ap, err := scanApproval(rows)
		if err != nil {
			return nil, fmt.Errorf("scan approval: %w", err)
		}
		out = append(out, *ap)
	}
	return out, rows.Err()
}

// Decide records a decision (allow/deny) on an approval that is still
// pending. The UPDATE is guarded by "AND state = 'pending'" so a decision
// can never silently overwrite an approval someone (or something, e.g. the
// scheduler's expiry sweep) has already decided: when no row matches,
// Decide distinguishes "no such approval" (domain.ErrNotFound) from
// "approval exists but is no longer pending" (domain.ErrConflict), so
// callers can tell a lost race from a bad id.
func (a *Approvals) Decide(ctx context.Context, id string, state domain.ApprovalState, by string, updated json.RawMessage, msg string) error {
	res, err := a.d.ExecContext(ctx, `
		UPDATE approvals SET state = ?, decided_by = ?, decided_at = ?, updated_input = ?, message = ?
		WHERE id = ? AND state = 'pending'`,
		string(state), by, nowString(time.Now()), optionalJSON(updated), msg, id)
	if err != nil {
		return fmt.Errorf("decide approval: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, getErr := a.Get(ctx, id); getErr != nil {
			return getErr
		}
		return fmt.Errorf("decide approval: %w", domain.ErrConflict)
	}
	return nil
}

// Snooze sets snoozed_until on a pending approval.
func (a *Approvals) Snooze(ctx context.Context, id string, until time.Time) error {
	res, err := a.d.ExecContext(ctx, `UPDATE approvals SET snoozed_until = ? WHERE id = ?`, nowString(until), id)
	if err != nil {
		return fmt.Errorf("snooze approval: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("snooze approval: %w", domain.ErrNotFound)
	}
	return nil
}

// ExpireBefore flips every pending approval created before t to expired,
// returning the number of rows changed.
func (a *Approvals) ExpireBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := a.d.ExecContext(ctx, `UPDATE approvals SET state = 'expired' WHERE state = 'pending' AND created_at < ?`, nowString(t))
	if err != nil {
		return 0, fmt.Errorf("expire approvals: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire approvals: %w", err)
	}
	return n, nil
}
