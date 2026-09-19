package schedules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

// tickInterval is how often Run calls Tick.
const tickInterval = 30 * time.Second

// RunNow starts a schedule's run immediately, ignoring its cron and
// next_run_at: an explicit operator action always fires (it does not
// check for an overlapping run, unlike a tick).
func (s *Service) RunNow(ctx context.Context, actor Actor, id string) (domain.Run, error) {
	sc, err := s.mutableSchedule(ctx, actor, id, "run")
	if err != nil {
		return domain.Run{}, err
	}
	return s.startAndRecord(ctx, sc, s.now())
}

// Run ticks every 30s until ctx is done.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx, s.now())
		}
	}
}

// Tick loads every schedule due at or before now and fires each: started,
// skipped_overlap when its previous run is still going, or failed. Every
// due schedule gets its next_run_at recomputed, whatever the outcome.
func (s *Service) Tick(ctx context.Context, now time.Time) {
	due, err := s.repos.Schedules.ListDue(ctx, now)
	if err != nil {
		s.logger.Error("schedules: list due", "error", err)
		return
	}
	for _, sc := range due {
		s.tickOne(ctx, sc, now)
	}
}

// tickOne fires (or skips) one due schedule and recomputes its next run.
func (s *Service) tickOne(ctx context.Context, sc domain.Schedule, now time.Time) {
	if s.overlapping(ctx, sc.ID) {
		s.recordFiring(ctx, sc.ID, now, domain.FiringSkippedOverlap, "the previous run is still running", nil)
	} else if _, err := s.startAndRecord(ctx, sc, now); err != nil {
		s.logger.Warn("schedules: tick did not start a run", "schedule_id", sc.ID, "error", err)
	}

	sched, err := parseCron(sc.Cron)
	if err != nil {
		// The cron expression validated at Create/Update time; a parse
		// failure here means it can no longer produce a next run, so the
		// schedule stops being picked up by ListDue rather than looping.
		s.logger.Error("schedules: parse cron on tick", "schedule_id", sc.ID, "cron", sc.Cron, "error", err)
		if sErr := s.repos.Schedules.SetNextRun(ctx, sc.ID, nil); sErr != nil {
			s.logger.Error("schedules: clear next run", "schedule_id", sc.ID, "error", sErr)
		}
		return
	}
	next := sched.Next(now)
	if err := s.repos.Schedules.SetNextRun(ctx, sc.ID, &next); err != nil {
		s.logger.Error("schedules: set next run", "schedule_id", sc.ID, "error", err)
	}
}

// overlapping reports whether the run a schedule last started is still
// running. Any lookup failure (no prior firing, the run row is gone, the
// lookup errors) is treated as "not overlapping": a schedule must never
// get stuck skipping forever because of a transient error.
func (s *Service) overlapping(ctx context.Context, scheduleID string) bool {
	runID, err := s.repos.Firings.LastStartedRunID(ctx, scheduleID)
	if err != nil {
		return false
	}
	if ref, isPipeline := strings.CutPrefix(runID, PipelineRunRefPrefix); isPipeline {
		// A pipeline run is not a run row, so RunLookup cannot resolve the
		// reference: the pipeline-side reader answers instead. Without one
		// (a Service wired without WithPipelineRuns) the schedule keeps the
		// pre-v0.5 behaviour and never skips.
		if s.pipelineRuns == nil {
			return false
		}
		running, err := s.pipelineRuns.IsRunning(ctx, ref)
		if err != nil {
			return false
		}
		return running
	}
	view, err := s.lookup.Get(ctx, runID)
	if err != nil {
		return false
	}
	return view.Run.Outcome == domain.RunRunning
}

// startAndRecord renders a schedule's vars, starts a run through the
// engine, and records both a firing and the schedule's last_run_at/
// last_outcome. Used by both Tick (via tickOne) and RunNow.
func (s *Service) startAndRecord(ctx context.Context, sc domain.Schedule, firedAt time.Time) (domain.Run, error) {
	vars, err := mergeVars(sc.Vars, sc.Name, firedAt)
	if err != nil {
		s.finishFiring(ctx, sc.ID, firedAt, domain.FiringFailed, err.Error(), nil, "failed")
		return domain.Run{}, err
	}

	if sc.PipelineID != nil {
		return s.startPipeline(ctx, sc, vars, firedAt)
	}

	run, err := s.starter.Start(ctx, RunInput{
		TemplateID: sc.TemplateID,
		Vars:       vars,
		Origin:     domain.OriginSchedule,
	})
	if err != nil {
		s.finishFiring(ctx, sc.ID, firedAt, domain.FiringFailed, err.Error(), nil, "failed")
		return domain.Run{}, err
	}

	runID := run.ID
	s.finishFiring(ctx, sc.ID, firedAt, domain.FiringStarted, "", &runID, "started")
	return run, nil
}

// startPipeline fires a schedule that names a pipeline instead of a
// template. The firing's run_id records the pipeline run id behind the
// PipelineRunRefPrefix, and the Run returned to the caller (RunNow's
// response) is a stand-in carrying that same prefixed id: a pipeline run is
// not a run row, but the scheduler's API predates pipelines.
func (s *Service) startPipeline(ctx context.Context, sc domain.Schedule, vars templates.Vars, firedAt time.Time) (domain.Run, error) {
	if s.pipelines == nil {
		const reason = "pipelines are not available"
		s.finishFiring(ctx, sc.ID, firedAt, domain.FiringFailed, reason, nil, "failed")
		return domain.Run{}, fmt.Errorf("%w: %s", domain.ErrConflict, reason)
	}
	pr, err := s.pipelines.Start(ctx, serviceActor, *sc.PipelineID, vars, domain.OriginSchedule, sc.ID)
	if err != nil {
		s.finishFiring(ctx, sc.ID, firedAt, domain.FiringFailed, err.Error(), nil, "failed")
		return domain.Run{}, err
	}
	ref := PipelineRunRefPrefix + pr.ID
	s.finishFiring(ctx, sc.ID, firedAt, domain.FiringStarted, "", &ref, "started")
	return domain.Run{ID: ref, Origin: string(domain.OriginSchedule), StartedAt: pr.StartedAt, Outcome: domain.RunRunning}, nil
}

// finishFiring records a firing and stamps the schedule's last_run_at/
// last_outcome, logging (rather than failing the caller on) either write:
// the run itself already started or failed by the time this is called, so
// a bookkeeping error must not be mistaken for that outcome.
func (s *Service) finishFiring(ctx context.Context, scheduleID string, firedAt time.Time, status domain.ScheduleFiringStatus, reason string, runID *string, outcome string) {
	s.recordFiring(ctx, scheduleID, firedAt, status, reason, runID)
	if err := s.repos.Schedules.RecordRun(ctx, scheduleID, firedAt, outcome); err != nil {
		s.logger.Error("schedules: record run", "schedule_id", scheduleID, "error", err)
	}
}

// recordFiring writes one schedule_firings row, logging rather than
// returning an error: a failed firing log must not be conflated with the
// run start/skip decision it is describing.
func (s *Service) recordFiring(ctx context.Context, scheduleID string, firedAt time.Time, status domain.ScheduleFiringStatus, reason string, runID *string) {
	f := domain.ScheduleFiring{
		ID:         uuid.NewString(),
		ScheduleID: scheduleID,
		FiredAt:    firedAt,
		Status:     status,
		Reason:     reason,
		RunID:      runID,
	}
	if err := s.repos.Firings.Create(ctx, f); err != nil {
		s.logger.Error("schedules: record firing", "schedule_id", scheduleID, "status", status, "error", err)
	}
}

// mergeVars decodes a schedule's stored vars object and adds the
// `schedule` key the plan documents: {name, fired_at}.
func mergeVars(raw json.RawMessage, name string, firedAt time.Time) (templates.Vars, error) {
	vars := templates.Vars{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &vars); err != nil {
			return nil, fmt.Errorf("%w: schedule vars: %v", domain.ErrInvalid, err)
		}
	}
	vars["schedule"] = map[string]any{
		"name":     name,
		"fired_at": firedAt.UTC().Format(time.RFC3339),
	}
	return vars, nil
}
