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

// seedPipelineRunFixtures creates the workspace and pipeline a pipeline
// run's pipeline_id foreign key needs, returning the pipeline id.
func seedPipelineRunFixtures(t *testing.T, d *DB) string {
	t.Helper()
	seedTemplateFixtures(t, d)
	pl := newTestPipeline("ws1", "fix-ci")
	if err := NewPipelines(d).Create(context.Background(), pl); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	return pl.ID
}

func newTestPipelineRun(pipelineID string, state domain.PipelineRunState, startedAt time.Time) domain.PipelineRun {
	return domain.PipelineRun{
		ID:         uuid.NewString(),
		PipelineID: pipelineID,
		Origin:     "ui",
		State:      state,
		StartedAt:  startedAt,
	}
}

func TestPipelineRuns_CreateGetFinish(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	plID := seedPipelineRunFixtures(t, database)
	runs := NewPipelineRuns(database)

	r := newTestPipelineRun(plID, domain.PipelineRunRunning, time.Now())
	r.Input = json.RawMessage(`{"payload":{"title":"x"}}`)
	if err := runs.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PipelineID != plID || got.State != domain.PipelineRunRunning || got.FinishedAt != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, r)
	}
	if string(got.Input) != string(r.Input) {
		t.Fatalf("Input = %s, want %s", got.Input, r.Input)
	}

	if err := runs.Finish(ctx, r.ID, domain.PipelineRunSuccess, 1.25); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got, err = runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get after Finish: %v", err)
	}
	if got.State != domain.PipelineRunSuccess || got.FinishedAt == nil || got.CostUSD != 1.25 {
		t.Fatalf("Get after Finish = %+v", got)
	}

	if err := runs.Finish(ctx, "no-such-id", domain.PipelineRunSuccess, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Finish unknown id: err = %v, want ErrNotFound", err)
	}
	if _, err := runs.Get(ctx, "no-such-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestPipelineRuns_CreateDefaultsEmptyInput(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	plID := seedPipelineRunFixtures(t, database)
	runs := NewPipelineRuns(database)

	r := newTestPipelineRun(plID, domain.PipelineRunRunning, time.Now())
	r.Input = nil
	if err := runs.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Input) != "{}" {
		t.Fatalf("Input = %q, want %q", got.Input, "{}")
	}
}

func TestPipelineRuns_List(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	plID := seedPipelineRunFixtures(t, database)
	runs := NewPipelineRuns(database)

	now := time.Now().Truncate(time.Second)
	running := newTestPipelineRun(plID, domain.PipelineRunRunning, now.Add(-time.Minute))
	success := newTestPipelineRun(plID, domain.PipelineRunSuccess, now)
	for _, r := range []domain.PipelineRun{running, success} {
		if err := runs.Create(ctx, r); err != nil {
			t.Fatalf("Create %s: %v", r.ID, err)
		}
	}

	all, err := runs.List(ctx, domain.PipelineRunFilter{PipelineID: plID})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List all len = %d, want 2", len(all))
	}
	// newest first
	if all[0].ID != success.ID || all[1].ID != running.ID {
		t.Fatalf("List all order = %+v, want [success, running]", all)
	}

	filtered, err := runs.List(ctx, domain.PipelineRunFilter{PipelineID: plID, State: string(domain.PipelineRunRunning)})
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != running.ID {
		t.Fatalf("List filtered = %+v, want only %q", filtered, running.ID)
	}

	limited, err := runs.List(ctx, domain.PipelineRunFilter{PipelineID: plID, Limit: 1})
	if err != nil {
		t.Fatalf("List limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("List limited len = %d, want 1", len(limited))
	}
}

func TestPipelineRuns_ListRunningOlderThan(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	plID := seedPipelineRunFixtures(t, database)
	runs := NewPipelineRuns(database)

	now := time.Now().Truncate(time.Second)
	old := newTestPipelineRun(plID, domain.PipelineRunRunning, now.Add(-time.Hour))
	recent := newTestPipelineRun(plID, domain.PipelineRunRunning, now)
	finished := newTestPipelineRun(plID, domain.PipelineRunRunning, now.Add(-time.Hour))
	for _, r := range []domain.PipelineRun{old, recent, finished} {
		if err := runs.Create(ctx, r); err != nil {
			t.Fatalf("Create %s: %v", r.ID, err)
		}
	}
	if err := runs.Finish(ctx, finished.ID, domain.PipelineRunSuccess, 0); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	cutoff := now.Add(-time.Minute)
	stale, err := runs.ListRunningOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListRunningOlderThan: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != old.ID {
		t.Fatalf("ListRunningOlderThan = %+v, want only %q", stale, old.ID)
	}
}

func TestPipelineRuns_Reopen(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	plID := seedPipelineRunFixtures(t, database)
	runs := NewPipelineRuns(database)

	r := newTestPipelineRun(plID, domain.PipelineRunRunning, time.Now())
	if err := runs.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := runs.Finish(ctx, r.ID, domain.PipelineRunFailed, 2.5); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if err := runs.Reopen(ctx, r.ID); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	got, err := runs.Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get after Reopen: %v", err)
	}
	if got.State != domain.PipelineRunRunning {
		t.Fatalf("State after Reopen = %q, want running", got.State)
	}
	if got.FinishedAt != nil {
		t.Fatalf("FinishedAt after Reopen = %v, want nil", got.FinishedAt)
	}
	// The attempts already paid for still count toward the retried run.
	if got.CostUSD != 2.5 {
		t.Fatalf("CostUSD after Reopen = %v, want 2.5", got.CostUSD)
	}

	if err := runs.Reopen(ctx, "no-such-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Reopen unknown id: err = %v, want ErrNotFound", err)
	}
}
