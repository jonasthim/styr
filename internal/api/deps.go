// Package api wires Styr's HTTP surface: a chi router, middleware, JSON
// helpers and one *_handlers.go file per resource. Handlers call services;
// they contain no business logic beyond decoding, role checks and shaping
// responses.
package api

import (
	"context"
	"time"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/sessions"
)

// TokenVerifier checks that a Claude token actually works before Styr
// stores it. The real implementation (Task 14) runs the claude CLI; tests
// use a stub.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) error
}

// StatusInfo is the payload for GET /api/v1/status.
type StatusInfo struct {
	Version       string `json:"version"`
	ClaudeVersion string `json:"claude_version"`
	OpenProcesses int    `json:"open_processes"`
	Slots         int    `json:"slots"`
	QueueDepth    int    `json:"queue_depth"`
}

// Deps is everything the API handlers need.
type Deps struct {
	Auth       *auth.Service
	Sessions   *sessions.Service
	Users      *db.Users
	Tokens     *db.Tokens
	Workspaces *db.Workspaces
	Profiles   *db.Profiles
	Audit      *db.Audit
	Bus        *events.Bus
	Box        *crypto.Box
	Verifier   TokenVerifier
	Status     func() StatusInfo
	Version    string

	// MaxOpenSessions and IdleTimeout surface the sessions scheduler's
	// configured limits on GET /api/v1/settings. sessions.Service does not
	// expose its Options (they are private to the scheduler), so the
	// composition root (Task 14) passes the same values it gave
	// sessions.New here.
	MaxOpenSessions int
	IdleTimeout     time.Duration
}
