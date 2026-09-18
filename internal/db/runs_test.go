package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// seedRunFixtures creates a workspace/profile and a session for a run's
// session_id foreign key, returning the session id.
func seedRunFixtures(t *testing.T, d *DB) string {
	t.Helper()
	ctx := context.Background()
	seedTemplateFixtures(t, d)
	sess := domain.Session{
		ID: uuid.NewString(), Title: "unattended run", WorkspaceID: "ws1", ProfileID: "investigate",
		Harness: "claude", State: domain.SessionRunning, Origin: domain.OriginWebhook,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := NewSessions(d).Create(ctx, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return sess.ID
}

func newTestRun(sessionID string, outcome domain.RunOutcome, startedAt time.Time) domain.Run {
	return domain.Run{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Origin:    "webhook",
		StartedAt: startedAt,
		Outcome:   outcome,
	}
}

func TestRuns_CreateGetGetBySessionFinish(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	sessID := seedRunFixtures(t, database)
	runs := NewRuns(database)

	r := newTestRun(sessID, domain.RunRunning, time.Now())
	if err := runs.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Outcome != domain.RunRunning || got.FinishedAt != nil || got.SessionID != sessID {
		t.Fatalf("Get = %+v, want matching %+v", got, r)
	}
	// A run with no report yet must come back with a nil (not merely empty)
	// RawMessage: an empty one is not valid JSON, and marshalling it fails,
	// which would make every API response carrying a running run a 500.
	if got.Report != nil {
		t.Fatalf("Get report = %q, want nil for a run with no report yet", got.Report)
	}
	if _, err := json.Marshal(got); err != nil {
		t.Fatalf("Marshal a run with no report: %v", err)
	}

	bySession, err := runs.GetBySession(ctx, sessID)
	if err != nil {
		t.Fatalf("GetBySession: %v", err)
	}
	if bySession.ID != r.ID {
		t.Fatalf("GetBySession id = %s, want %s", bySession.ID, r.ID)
	}
	if _, err := runs.GetBySession(ctx, "no-such-session"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetBySession unknown: err = %v, want ErrNotFound", err)
	}

	report := json.RawMessage(`{"severity":"warning","diagnosis":"disk full"}`)
	if err := runs.Finish(ctx, r.ID, domain.RunSuccess, report, "disk full", 0.42); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got, err = runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get after Finish: %v", err)
	}
	if got.Outcome != domain.RunSuccess || got.FinishedAt == nil || got.Summary != "disk full" || got.CostUSD != 0.42 {
		t.Fatalf("Get after Finish = %+v", got)
	}
	if string(got.Report) != string(report) {
		t.Fatalf("Get after Finish report = %s, want %s", got.Report, report)
	}

	if err := runs.Finish(ctx, "no-such-id", domain.RunFailed, nil, "", 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Finish unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestRuns_List_FilterByOutcomeAndTrigger(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	sessID := seedRunFixtures(t, database)
	runs := NewRuns(database)

	// seedRunFixtures already created the "ws1" workspace this test's
	// session uses; reuse it for the template/trigger a run's trigger_id
	// foreign key needs, rather than seeding it again.
	tpl := newTestTemplate("run-filter-template")
	if err := NewTemplates(database).Create(ctx, tpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	trig := newTestTrigger(tpl.ID, "run-filter-trigger", "run-filter-trigger-slug")
	if err := NewTriggers(database).Create(ctx, trig); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	trigID := trig.ID
	r1 := newTestRun(sessID, domain.RunSuccess, time.Now().Add(-2*time.Minute))
	r1.TriggerID = &trigID
	r2 := newTestRun(sessID, domain.RunFailed, time.Now().Add(-1*time.Minute))
	r2.TriggerID = &trigID
	r3 := newTestRun(sessID, domain.RunSuccess, time.Now())
	for _, r := range []domain.Run{r1, r2, r3} {
		if err := runs.Create(ctx, r); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	byOutcome, err := runs.List(ctx, domain.RunFilter{Outcome: "success"})
	if err != nil {
		t.Fatalf("List by outcome: %v", err)
	}
	if len(byOutcome) != 2 {
		t.Fatalf("List by outcome len = %d, want 2", len(byOutcome))
	}
	// Newest first.
	if byOutcome[0].ID != r3.ID {
		t.Fatalf("List by outcome[0] = %s, want newest %s", byOutcome[0].ID, r3.ID)
	}

	byTrigger, err := runs.List(ctx, domain.RunFilter{TriggerID: trigID})
	if err != nil {
		t.Fatalf("List by trigger: %v", err)
	}
	if len(byTrigger) != 2 {
		t.Fatalf("List by trigger len = %d, want 2", len(byTrigger))
	}

	limited, err := runs.List(ctx, domain.RunFilter{Limit: 1})
	if err != nil {
		t.Fatalf("List limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("List limited len = %d, want 1", len(limited))
	}
}

func TestRuns_ListRunningOlderThan(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	sessID := seedRunFixtures(t, database)
	runs := NewRuns(database)

	old := newTestRun(sessID, domain.RunRunning, time.Now().Add(-2*time.Hour))
	recent := newTestRun(sessID, domain.RunRunning, time.Now())
	if err := runs.Create(ctx, old); err != nil {
		t.Fatalf("Create old: %v", err)
	}
	if err := runs.Create(ctx, recent); err != nil {
		t.Fatalf("Create recent: %v", err)
	}
	// A second session so a non-running run for the recent one doesn't
	// interfere.
	if err := runs.Finish(ctx, recent.ID, domain.RunSuccess, nil, "", 0); err != nil {
		t.Fatalf("Finish recent: %v", err)
	}

	cutoff := time.Now().Add(-30 * time.Minute)
	stale, err := runs.ListRunningOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListRunningOlderThan: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != old.ID {
		t.Fatalf("ListRunningOlderThan = %+v, want only %s", stale, old.ID)
	}
}

func TestRuns_LoopColumnsListByLoopAndFilter(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	templateID, sessID := seedLoopFixtures(t, database)
	loops := NewLoops(database)
	runs := NewRuns(database)

	now := time.Now()
	loopID := uuid.NewString()
	if err := loops.Create(ctx, domain.Loop{ID: loopID, TemplateID: templateID, SessionID: &sessID,
		Origin: "webhook", UntilField: "done", MaxIterations: 3, Iteration: 1,
		State: domain.LoopRunning, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create loop: %v", err)
	}

	// Two iterations of the loop on one session, plus one unrelated run.
	second := newTestRun(sessID, domain.RunRunning, now.Add(time.Second))
	second.LoopID, second.Iteration = loopID, 2
	first := newTestRun(sessID, domain.RunSuccess, now)
	first.LoopID, first.Iteration = loopID, 1
	other := newTestRun(sessID, domain.RunSuccess, now.Add(-time.Hour))

	for _, r := range []domain.Run{second, first, other} {
		if err := runs.Create(ctx, r); err != nil {
			t.Fatalf("create run: %v", err)
		}
	}

	got, err := runs.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LoopID != loopID || got.Iteration != 1 {
		t.Fatalf("Get = loop %q iteration %d", got.LoopID, got.Iteration)
	}
	if got, err := runs.Get(ctx, other.ID); err != nil || got.LoopID != "" || got.Iteration != 0 {
		t.Fatalf("a run outside a loop = %+v (err %v)", got, err)
	}

	chain, err := runs.ListByLoop(ctx, loopID)
	if err != nil {
		t.Fatalf("ListByLoop: %v", err)
	}
	if len(chain) != 2 || chain[0].ID != first.ID || chain[1].ID != second.ID {
		t.Fatalf("ListByLoop = %+v, want the two iterations in order", chain)
	}

	filtered, err := runs.List(ctx, domain.RunFilter{LoopID: loopID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("List by loop = %d runs, want 2", len(filtered))
	}

	// All three runs share a session; the newest iteration is the one a
	// live session event belongs to.
	bySession, err := runs.GetBySession(ctx, sessID)
	if err != nil {
		t.Fatalf("GetBySession: %v", err)
	}
	if bySession.ID != second.ID {
		t.Fatalf("GetBySession = %q, want the newest iteration %q", bySession.ID, second.ID)
	}
}

// TestRuns_StepRunIDRoundTrip checks the step_run_id column T53 added: nil
// for an ordinary run, and set for a run started as one attempt of a
// pipeline step (see internal/domain.Run.StepRunID).
func TestRuns_StepRunIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)

	// Build the session and step-run fixtures directly rather than via
	// seedRunFixtures/seedStepRunFixtures, which would each seed the "ws1"
	// workspace and conflict.
	seedTemplateFixtures(t, database)
	sess := domain.Session{
		ID: uuid.NewString(), Title: "pipeline step run", WorkspaceID: "ws1", ProfileID: "investigate",
		Harness: "claude", State: domain.SessionRunning, Origin: domain.OriginPipeline,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := NewSessions(database).Create(ctx, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	pl := newTestPipeline("ws1", "fix-ci")
	if err := NewPipelines(database).Create(ctx, pl); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	pr := newTestPipelineRun(pl.ID, domain.PipelineRunRunning, time.Now())
	if err := NewPipelineRuns(database).Create(ctx, pr); err != nil {
		t.Fatalf("seed pipeline run: %v", err)
	}
	sr := newTestStepRun(pr.ID, "triage")
	if err := NewStepRuns(database).Create(ctx, sr); err != nil {
		t.Fatalf("seed step run: %v", err)
	}

	runs := NewRuns(database)
	r := newTestRun(sess.ID, domain.RunRunning, time.Now())
	r.Origin = "pipeline"
	r.StepRunID = &sr.ID
	if err := runs.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.StepRunID == nil || *got.StepRunID != sr.ID {
		t.Fatalf("StepRunID = %v, want %q", got.StepRunID, sr.ID)
	}

	plain := newTestRun(sess.ID, domain.RunRunning, time.Now())
	if err := runs.Create(ctx, plain); err != nil {
		t.Fatalf("Create plain: %v", err)
	}
	got, err = runs.Get(ctx, plain.ID)
	if err != nil {
		t.Fatalf("Get plain: %v", err)
	}
	if got.StepRunID != nil {
		t.Fatalf("StepRunID = %v, want nil for an ordinary run", got.StepRunID)
	}
}
