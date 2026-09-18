package domain

import "time"

// APIToken is a personal access token (`styr_pat_…`) a user can use as a
// bearer credential instead of a cookie session. TokenHash (sha256 of the
// token) is never marshalled to JSON — the plaintext token is shown to the
// user once, at creation, and never stored.
type APIToken struct {
	ID     string
	UserID string
	Name   string

	TokenHash string `json:"-"`
	Prefix    string

	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
}
