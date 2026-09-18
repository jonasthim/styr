// Package triggers is the inbound side of unattended runs: it owns
// templates and trigger endpoints, and it is the `/hooks/{slug}` pipeline —
// authenticate, normalise, dedupe, rate-limit, log the delivery, start the
// run. Every decision it makes is recorded as a delivery row, so an
// operator can always see why an alert did or did not start a session.
package triggers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/runs"
	"github.com/jonasthim/styr/internal/sessions"
)

// Actor identifies who is calling the service. Shared with
// internal/sessions so the API layer passes one actor everywhere.
type Actor = sessions.Actor

// RunInput is what the router hands the run engine for an accepted
// delivery. Aliased from internal/runs so *runs.Engine satisfies
// RunStarter without triggers redefining the engine's input.
type RunInput = runs.RunInput

// RunStarter starts an unattended run. *runs.Engine implements it; tests
// substitute a recorder.
type RunStarter interface {
	Start(ctx context.Context, in RunInput) (domain.Run, error)
}

// Repos bundles the repositories the service reads and writes.
type Repos struct {
	Templates  *db.Templates
	Triggers   *db.Triggers
	Deliveries *db.Deliveries
}

// Service owns templates, triggers and the inbound delivery pipeline.
type Service struct {
	repos  Repos
	engine RunStarter
	logger *slog.Logger
	now    func() time.Time

	// box is held for symmetry with the other services that store sealed
	// secrets. Trigger secrets are stored as a sha256 hash, never sealed
	// (see ADR-010), so nothing in this package opens the box today.
	box *crypto.Box
}

// New constructs a Service. now is the clock used for delivery timestamps
// and the cooldown/dedupe windows (nil means time.Now); logger may be nil.
func New(repos Repos, engine RunStarter, box *crypto.Box, logger *slog.Logger, now func() time.Time) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Service{repos: repos, engine: engine, box: box, logger: logger, now: now}
}

// visibleOwner reports whether actor may see a row owned by owner: admins
// see everything, and everyone sees shared (owner-less) rows. Same rule as
// workspaces and sessions.
func visibleOwner(actor Actor, owner *string) bool {
	return actor.IsAdmin || owner == nil || *owner == actor.UserID
}

// mutableBy reports whether actor may change a row owned by owner: its
// owner, or an admin. A shared row is admin-only.
func mutableBy(actor Actor, owner *string) bool {
	return actor.IsAdmin || (owner != nil && *owner == actor.UserID)
}

// ownerFor returns the owner a newly created row gets: nil (shared) only
// when an admin explicitly asked for it, otherwise the actor.
func ownerFor(actor Actor, shared bool) *string {
	if actor.IsAdmin && shared {
		return nil
	}
	id := actor.UserID
	return &id
}

// wrapConstraint turns SQLite's foreign-key complaint into
// domain.ErrInvalid, so a create or update naming a workspace, profile or
// template that does not exist reads as a 422 rather than a 500.
func wrapConstraint(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "FOREIGN KEY constraint") {
		return fmt.Errorf("%w: unknown workspace, profile or template", domain.ErrInvalid)
	}
	return err
}
