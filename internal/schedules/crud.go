package schedules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
)

// defaultFiringLimit and maxFiringLimit bound Firings.
const (
	defaultFiringLimit = 50
	maxFiringLimit     = 500
)

// Create validates in, computes the schedule's first next_run_at from its
// cron expression, and stores it.
func (s *Service) Create(ctx context.Context, actor Actor, in domain.ScheduleInput) (domain.Schedule, error) {
	if strings.TrimSpace(in.Name) == "" {
		return domain.Schedule{}, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	if in.TemplateID == "" {
		return domain.Schedule{}, fmt.Errorf("%w: template_id is required", domain.ErrInvalid)
	}
	sched, err := parseCron(in.Cron)
	if err != nil {
		return domain.Schedule{}, err
	}
	vars, err := validateVars(in.Vars)
	if err != nil {
		return domain.Schedule{}, err
	}

	now := s.now()
	next := sched.Next(now)
	sc := domain.Schedule{
		ID:         uuid.NewString(),
		OwnerID:    ownerFor(actor, in.Shared),
		Name:       strings.TrimSpace(in.Name),
		TemplateID: in.TemplateID,
		Cron:       in.Cron,
		Enabled:    in.Enabled == nil || *in.Enabled,
		Vars:       vars,
		NextRunAt:  &next,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repos.Schedules.Create(ctx, sc); err != nil {
		return domain.Schedule{}, wrapConstraint(err)
	}
	return sc, nil
}

// List returns every schedule visible to actor.
func (s *Service) List(ctx context.Context, actor Actor) ([]domain.Schedule, error) {
	return s.repos.Schedules.ListVisible(ctx, actor.UserID, actor.IsAdmin)
}

// Get returns one schedule, or domain.ErrNotFound when actor cannot see
// it.
func (s *Service) Get(ctx context.Context, actor Actor, id string) (domain.Schedule, error) {
	sc, err := s.repos.Schedules.Get(ctx, id)
	if err != nil {
		return domain.Schedule{}, err
	}
	if !visibleOwner(actor, sc.OwnerID) {
		return domain.Schedule{}, fmt.Errorf("schedule %s: %w", id, domain.ErrNotFound)
	}
	return *sc, nil
}

// Update replaces a schedule's mutable configuration. A cron change
// recomputes next_run_at from now; leaving the cron unchanged leaves the
// schedule's existing next_run_at (and therefore its cadence) alone.
func (s *Service) Update(ctx context.Context, actor Actor, id string, in domain.ScheduleInput) (domain.Schedule, error) {
	existing, err := s.mutableSchedule(ctx, actor, id, "update")
	if err != nil {
		return domain.Schedule{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return domain.Schedule{}, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	if in.TemplateID == "" {
		return domain.Schedule{}, fmt.Errorf("%w: template_id is required", domain.ErrInvalid)
	}
	sched, err := parseCron(in.Cron)
	if err != nil {
		return domain.Schedule{}, err
	}
	vars, err := validateVars(in.Vars)
	if err != nil {
		return domain.Schedule{}, err
	}

	updated := existing
	if actor.IsAdmin && in.Shared {
		updated.OwnerID = nil
	}
	updated.Name = strings.TrimSpace(in.Name)
	updated.TemplateID = in.TemplateID
	cronChanged := updated.Cron != in.Cron
	updated.Cron = in.Cron
	updated.Vars = vars
	if in.Enabled != nil {
		updated.Enabled = *in.Enabled
	}
	updated.UpdatedAt = s.now()
	if cronChanged {
		next := sched.Next(s.now())
		updated.NextRunAt = &next
	}

	if err := s.repos.Schedules.Update(ctx, updated); err != nil {
		return domain.Schedule{}, wrapConstraint(err)
	}
	if cronChanged {
		if err := s.repos.Schedules.SetNextRun(ctx, updated.ID, updated.NextRunAt); err != nil {
			return domain.Schedule{}, err
		}
	}
	return updated, nil
}

// Delete removes a schedule and, by cascade, its firings.
func (s *Service) Delete(ctx context.Context, actor Actor, id string) error {
	if _, err := s.mutableSchedule(ctx, actor, id, "delete"); err != nil {
		return err
	}
	return s.repos.Schedules.Delete(ctx, id)
}

// Firings returns the newest firings for a schedule the actor can see.
func (s *Service) Firings(ctx context.Context, actor Actor, id string, limit int) ([]domain.ScheduleFiring, error) {
	if _, err := s.Get(ctx, actor, id); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultFiringLimit
	}
	if limit > maxFiringLimit {
		limit = maxFiringLimit
	}
	return s.repos.Firings.ListBySchedule(ctx, id, limit)
}

// mutableSchedule loads a schedule actor may change, or returns
// domain.ErrNotFound (invisible) / domain.ErrForbidden (visible but not
// theirs). verb names the attempted operation in the error message.
func (s *Service) mutableSchedule(ctx context.Context, actor Actor, id, verb string) (domain.Schedule, error) {
	sc, err := s.Get(ctx, actor, id)
	if err != nil {
		return domain.Schedule{}, err
	}
	if !mutableBy(actor, sc.OwnerID) {
		return domain.Schedule{}, fmt.Errorf("%w: only the owner or an admin may %s this schedule", domain.ErrForbidden, verb)
	}
	return sc, nil
}

// validateVars normalises a schedule's stored vars payload, defaulting an
// empty one to '{}' and rejecting anything that is not a JSON object (the
// tick merges a `schedule` key into it, which requires an object).
func validateVars(raw json.RawMessage) (json.RawMessage, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return json.RawMessage("{}"), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: vars must be a JSON object: %v", domain.ErrInvalid, err)
	}
	return raw, nil
}
