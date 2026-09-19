// Package schedules is the cron side of unattended runs: it owns the
// schedules table, computes next-run times with robfig/cron, and ticks
// every 30s to start template runs through the existing runs.Engine — a
// schedule never overlaps itself, skipping a tick (recorded as a firing)
// when its previous run is still going.
package schedules

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/runs"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/templates"
)

// Actor identifies who is calling the service. Shared with
// internal/sessions and internal/triggers so the API layer passes one
// actor everywhere.
type Actor = sessions.Actor

// RunInput is what the service hands the run engine to start a schedule's
// run. Aliased from internal/runs so *runs.Engine satisfies RunStarter
// without this package redefining the engine's input.
type RunInput = runs.RunInput

// RunStarter starts an unattended run. *runs.Engine implements it; tests
// substitute a fake.
type RunStarter interface {
	Start(ctx context.Context, in RunInput) (domain.Run, error)
}

// RunLookup resolves a run by id, used to check whether the run a
// schedule last started is still running. *runs.Engine implements it;
// tests substitute a fake.
type RunLookup interface {
	Get(ctx context.Context, id string) (domain.RunView, error)
}

// PipelineStarter starts a pipeline run, for a schedule that names a
// pipeline instead of a template. *pipelines.Executor implements it; it is
// optional (nil until the composition root attaches one), and a schedule
// pointing at a pipeline without it records a failed firing.
type PipelineStarter interface {
	Start(ctx context.Context, actor Actor, pipelineID string, input templates.Vars,
		origin domain.Origin, originRef string) (domain.PipelineRun, error)
}

// PipelineRunRefPrefix marks a firing's run_id as a pipeline run id rather
// than a run id. The column has no foreign key (see migration 00006), so
// one column carries both; everything reading it must strip the prefix to
// know which table to look in.
const PipelineRunRefPrefix = "pr:"

// serviceActor is the actor the scheduler acts as when it starts a
// pipeline: a tick has no human behind it.
var serviceActor = Actor{IsAdmin: true}

// Repos bundles the repositories the service reads and writes.
type Repos struct {
	Schedules *db.Schedules
	Firings   *db.ScheduleFirings
}

// Service owns schedules and the scheduler tick.
type Service struct {
	repos   Repos
	starter RunStarter
	lookup  RunLookup
	// pipelines starts the pipeline runs of schedules that name one. Nil
	// until WithPipelines attaches the executor (the composition root wires
	// it after both services exist).
	pipelines PipelineStarter
	now       func() time.Time
	logger    *slog.Logger
}

// New constructs a Service. now is the clock used for next-run
// computation and firing timestamps (nil means time.Now); logger may be
// nil.
func New(repos Repos, starter RunStarter, lookup RunLookup, now func() time.Time, logger *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repos: repos, starter: starter, lookup: lookup, now: now, logger: logger}
}

// WithPipelines attaches the pipeline executor and returns s, so the
// composition root can wire the two services that reference each other.
func (s *Service) WithPipelines(p PipelineStarter) *Service {
	s.pipelines = p
	return s
}

// validateTarget enforces the plan's "exactly one of template_id and
// pipeline_id" rule on a schedule input.
func validateTarget(templateID, pipelineID string) error {
	switch {
	case templateID == "" && pipelineID == "":
		return fmt.Errorf("%w: one of template_id and pipeline_id is required", domain.ErrInvalid)
	case templateID != "" && pipelineID != "":
		return fmt.Errorf("%w: exactly one of template_id and pipeline_id may be set", domain.ErrInvalid)
	}
	return nil
}

// optionalID turns an empty id into a nil pointer, for the nullable
// pipeline_id column.
func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// visibleOwner reports whether actor may see a row owned by owner: admins
// see everything, and everyone sees shared (owner-less) rows. Same rule as
// workspaces, templates and triggers.
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
// domain.ErrInvalid, so a create or update naming a template that does
// not exist reads as a 422 rather than a 500.
func wrapConstraint(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "FOREIGN KEY constraint") {
		return fmt.Errorf("%w: unknown template", domain.ErrInvalid)
	}
	return err
}
