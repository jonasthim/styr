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
