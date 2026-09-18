package domain

import "time"

// ChannelKind is the delivery mechanism for a notification channel.
type ChannelKind string

const (
	ChannelNtfy    ChannelKind = "ntfy"
	ChannelWebhook ChannelKind = "webhook"
)

// NotificationChannel is an outbound target (ntfy topic or generic
// webhook) that run events are published to. TokenCiphertext/TokenNonce
// hold an optional bearer token sealed with internal/crypto; they are
// never marshalled to JSON.
type NotificationChannel struct {
	ID   string
	Kind ChannelKind
	Name string
	URL  string

	TokenCiphertext []byte `json:"-"`
	TokenNonce      []byte `json:"-"`

	Events  []string // event kinds this channel is subscribed to
	Enabled bool

	CreatedAt time.Time
}
