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
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/workspaces"
)

// TokenVerifier checks that a Claude token actually works before Styr
// stores it. The real implementation (Task 14) runs the claude CLI; tests
// use a stub.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) error
}

// TokenStore is the subset of the api_tokens repository (T31's
// db.APITokens) the /me/api-tokens handlers need. Defined here, alongside
// auth.APITokenStore, rather than imported so this package does not depend
// on internal/db's concrete repository type; a nil TokenStore (the
// zero-value Deps, before T31's repository is wired in cmd/styr/wire.go)
// makes every /me/api-tokens handler answer 501 rather than panic.
type TokenStore interface {
	Create(ctx context.Context, t domain.APIToken) error
	ListByUser(ctx context.Context, userID string) ([]domain.APIToken, error)
	Delete(ctx context.Context, id, userID string) error
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
	Auth *auth.Service
	// Workspaces is the workspaces service handlers call for every
	// workspace route; WorkspacesRepo is kept for callers (and tests) that
	// need direct repository access, e.g. to seed a workspace bypassing
	// service-level validation.
	Workspaces     *workspaces.Service
	WorkspacesRepo *db.Workspaces
	Sessions       *sessions.Service
	Users          *db.Users
	Tokens         *db.Tokens
	Profiles       *db.Profiles
	Audit          *db.Audit
	Bus            *events.Bus
	Box            *crypto.Box
	Verifier       TokenVerifier
	// TokenStore backs GET/POST /me/api-tokens and DELETE
	// /me/api-tokens/{id} (T36). nil until T31's db.APITokens repository is
	// wired in (cmd/styr/wire.go); the handlers answer 501 in that case
	// rather than panicking.
	TokenStore TokenStore
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
