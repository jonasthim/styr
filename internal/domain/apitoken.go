package domain

import "time"

// APIToken is a personal API token: a long-lived bearer credential a user
// mints for scripts and automation, authenticated in place of the
// styr_session cookie (see internal/auth's Authenticate and
// GenerateAPIToken). Only TokenHash is stored at rest; the raw secret is
// shown to its owner once, at creation, and never persisted or logged.
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  string `json:"-"`
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
}
