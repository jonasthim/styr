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
	"github.com/jonasthim/styr/internal/templates"
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

// PipelineStarter starts a pipeline run, for a trigger that names a
// pipeline instead of a template. *pipelines.Executor implements it; it is
// optional (nil until the composition root attaches one), and a trigger
// pointing at a pipeline without it records a failed delivery rather than
// panicking.
type PipelineStarter interface {
	Start(ctx context.Context, actor Actor, pipelineID string, input templates.Vars,
		origin domain.Origin, originRef string) (domain.PipelineRun, error)
}

// PipelineRunRefPrefix marks a delivery's run_id as a pipeline run id
// rather than a run id. The column has no foreign key (see migration
// 00003), so one column carries both; everything reading it must strip the
// prefix to know which table to look in.
const PipelineRunRefPrefix = "pr:"

// serviceActor is the actor the router acts as when it starts a pipeline: a
// webhook delivery has no human behind it, exactly like the run engine's
// own unattended sessions.
var serviceActor = Actor{IsAdmin: true}

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
	// pipelines starts the pipeline runs of triggers that name one. Nil
	// until WithPipelines attaches the executor (the composition root wires
	// it after both services exist).
	pipelines PipelineStarter
	logger    *slog.Logger
	now       func() time.Time

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

// WithPipelines attaches the pipeline executor and returns s, so the
// composition root can wire the two services that reference each other.
func (s *Service) WithPipelines(p PipelineStarter) *Service {
	s.pipelines = p
	return s
}

// validateTarget enforces the plan's "exactly one of template_id and
// pipeline_id" rule on a trigger or schedule input.
func validateTarget(templateID, pipelineID string) error {
	switch {
	case templateID == "" && pipelineID == "":
		return fmt.Errorf("%w: one of template_id and pipeline_id is required", domain.ErrInvalid)
	case templateID != "" && pipelineID != "":
		return fmt.Errorf("%w: exactly one of template_id and pipeline_id may be set", domain.ErrInvalid)
	}
	return nil
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
