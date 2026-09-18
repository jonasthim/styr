package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
)

// NotificationChannels is the repository for the notification_channels
// table: outbound ntfy/webhook targets for run events.
type NotificationChannels struct{ d *DB }

// NewNotificationChannels constructs a NotificationChannels repository.
func NewNotificationChannels(d *DB) *NotificationChannels { return &NotificationChannels{d: d} }

const notificationChannelColumns = `id, kind, name, url, token_ciphertext, token_nonce, events, enabled, created_at`

// Create inserts a new notification channel row. c.ID must already be set.
func (n *NotificationChannels) Create(ctx context.Context, c domain.NotificationChannel) error {
	_, err := n.d.ExecContext(ctx, `
		INSERT INTO notification_channels (`+notificationChannelColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, string(c.Kind), c.Name, c.URL, blobArg(c.TokenCiphertext), blobArg(c.TokenNonce),
		marshalStringList(c.Events), boolToInt(c.Enabled), nowString(c.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create notification channel: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create notification channel: %w", err)
	}
	return nil
}

// blobArg passes a nil byte slice through as SQL NULL rather than an empty
// blob, so a channel without a token round-trips as no token at all.
func blobArg(b []byte) any {
	if b == nil {
		return nil
	}
	return b
}

func scanNotificationChannel(row interface{ Scan(dest ...any) error }) (*domain.NotificationChannel, error) {
	var (
		c         domain.NotificationChannel
		kind      string
		events    string
		enabled   int
		createdAt string
	)
	if err := row.Scan(&c.ID, &kind, &c.Name, &c.URL, &c.TokenCiphertext, &c.TokenNonce, &events, &enabled, &createdAt); err != nil {
		return nil, err
	}
	c.Kind = domain.ChannelKind(kind)
	c.Events = unmarshalStringList(events)
	c.Enabled = enabled != 0
	c.CreatedAt = parseTime(createdAt)
	return &c, nil
}

// Get loads a notification channel by id.
func (n *NotificationChannels) Get(ctx context.Context, id string) (*domain.NotificationChannel, error) {
	row := n.d.QueryRowContext(ctx, `SELECT `+notificationChannelColumns+` FROM notification_channels WHERE id = ?`, id)
	c, err := scanNotificationChannel(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get notification channel: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get notification channel: %w", err)
	}
	return c, nil
}

// List returns every notification channel, ordered by name.
func (n *NotificationChannels) List(ctx context.Context) ([]domain.NotificationChannel, error) {
	rows, err := n.d.QueryContext(ctx, `SELECT `+notificationChannelColumns+` FROM notification_channels ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.NotificationChannel
	for rows.Next() {
		c, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan notification channel: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// Update replaces a notification channel's mutable fields (everything but
// id and created_at).
func (n *NotificationChannels) Update(ctx context.Context, c domain.NotificationChannel) error {
	res, err := n.d.ExecContext(ctx, `
		UPDATE notification_channels SET kind = ?, name = ?, url = ?, token_ciphertext = ?, token_nonce = ?,
			events = ?, enabled = ? WHERE id = ?`,
		string(c.Kind), c.Name, c.URL, blobArg(c.TokenCiphertext), blobArg(c.TokenNonce),
		marshalStringList(c.Events), boolToInt(c.Enabled), c.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("update notification channel: %w", domain.ErrConflict)
		}
		return fmt.Errorf("update notification channel: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("update notification channel: %w", domain.ErrNotFound)
	}
	return nil
}

// Delete removes a notification channel by id.
func (n *NotificationChannels) Delete(ctx context.Context, id string) error {
	res, err := n.d.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete notification channel: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("delete notification channel: %w", domain.ErrNotFound)
	}
	return nil
}

// ListEnabledFor returns every enabled channel subscribed to eventKind
// (e.g. "run.finished"), matched against the JSON array in the events
// column via SQLite's bundled json_each table-valued function.
func (n *NotificationChannels) ListEnabledFor(ctx context.Context, eventKind string) ([]domain.NotificationChannel, error) {
	rows, err := n.d.QueryContext(ctx, `
		SELECT `+notificationChannelColumns+` FROM notification_channels
		WHERE enabled = 1 AND EXISTS (
			SELECT 1 FROM json_each(notification_channels.events) WHERE json_each.value = ?
		)
		ORDER BY name`, eventKind)
	if err != nil {
		return nil, fmt.Errorf("list notification channels enabled for %s: %w", eventKind, err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.NotificationChannel
	for rows.Next() {
		c, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan notification channel: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
