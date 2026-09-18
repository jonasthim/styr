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

// seedStepRunFixtures creates the workspace, pipeline and pipeline run a
// step run's pipeline_run_id foreign key needs, returning the pipeline
// run id.
func seedStepRunFixtures(t *testing.T, d *DB) string {
	t.Helper()
	ctx := context.Background()
	plID := seedPipelineRunFixtures(t, d)
	pr := newTestPipelineRun(plID, domain.PipelineRunRunning, time.Now())
	if err := NewPipelineRuns(d).Create(ctx, pr); err != nil {
		t.Fatalf("seed pipeline run: %v", err)
	}
	return pr.ID
}

// seedRunForStepAttach creates a session and run (step_runs.run_id
// references runs(id)) a step run can attach to via Update, returning the
// run's id.
func seedRunForStepAttach(t *testing.T, d *DB) string {
	t.Helper()
	ctx := context.Background()
	sess := domain.Session{
		ID: uuid.NewString(), Title: "pipeline step attempt", WorkspaceID: "ws1", ProfileID: "investigate",
		Harness: "claude", State: domain.SessionRunning, Origin: domain.OriginPipeline,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := NewSessions(d).Create(ctx, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	r := newTestRun(sess.ID, domain.RunRunning, time.Now())
	r.Origin = "pipeline"
	if err := NewRuns(d).Create(ctx, r); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return r.ID
}

func newTestStepRun(pipelineRunID, stepID string) domain.StepRun {
	return domain.StepRun{
		ID:            uuid.NewString(),
		PipelineRunID: pipelineRunID,
		StepID:        stepID,
		Attempt:       1,
		State:         domain.StepRunPending,
	}
}

func TestStepRuns_CreateGetUpdate(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	prID := seedStepRunFixtures(t, database)
	stepRuns := NewStepRuns(database)

	sr := newTestStepRun(prID, "triage")
	if err := stepRuns.Create(ctx, sr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := stepRuns.Get(ctx, sr.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PipelineRunID != prID || got.StepID != "triage" || got.State != domain.StepRunPending || got.RunID != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, sr)
	}
	if got.Report != nil {
		t.Fatalf("Report = %v, want nil for a step run with no report yet", got.Report)
	}

	runID := seedRunForStepAttach(t, database)
	report := json.RawMessage(`{"proposed_action":"revert"}`)
	started := time.Now().Truncate(time.Second)
	finished := started.Add(time.Minute)
	if err := stepRuns.Update(ctx, sr.ID, domain.StepRunSuccess, &runID, report, "/srv/wt-1", &started, &finished); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = stepRuns.Get(ctx, sr.ID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.State != domain.StepRunSuccess || got.RunID == nil || *got.RunID != runID {
		t.Fatalf("Get after Update = %+v", got)
	}
	if string(got.Report) != string(report) || got.Worktree != "/srv/wt-1" {
		t.Fatalf("Get after Update report/worktree = %+v", got)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(started.UTC()) || got.FinishedAt == nil || !got.FinishedAt.Equal(finished.UTC()) {
		t.Fatalf("Get after Update timestamps = %+v", got)
	}

	if err := stepRuns.Update(ctx, "no-such-id", domain.StepRunFailed, nil, nil, "", nil, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update unknown id: err = %v, want ErrNotFound", err)
	}
	if _, err := stepRuns.Get(ctx, "no-such-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestStepRuns_ListByPipelineRun(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	prID := seedStepRunFixtures(t, database)
	stepRuns := NewStepRuns(database)

	triage := newTestStepRun(prID, "triage")
	fix := newTestStepRun(prID, "fix")
	for _, sr := range []domain.StepRun{triage, fix} {
		if err := stepRuns.Create(ctx, sr); err != nil {
			t.Fatalf("Create %s: %v", sr.StepID, err)
		}
	}

	list, err := stepRuns.ListByPipelineRun(ctx, prID)
	if err != nil {
		t.Fatalf("ListByPipelineRun: %v", err)
	}
	if len(list) != 2 || list[0].StepID != "triage" || list[1].StepID != "fix" {
		t.Fatalf("ListByPipelineRun = %+v, want [triage, fix] in insertion order", list)
	}
}

func TestStepRuns_GetByRun(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	prID := seedStepRunFixtures(t, database)
	stepRuns := NewStepRuns(database)

	sr := newTestStepRun(prID, "triage")
	if err := stepRuns.Create(ctx, sr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	runID := seedRunForStepAttach(t, database)
	if err := stepRuns.Update(ctx, sr.ID, domain.StepRunRunning, &runID, nil, "", nil, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := stepRuns.GetByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetByRun: %v", err)
	}
	if got.ID != sr.ID {
		t.Fatalf("GetByRun id = %s, want %s", got.ID, sr.ID)
	}

	if _, err := stepRuns.GetByRun(ctx, "no-such-run"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByRun unknown run: err = %v, want ErrNotFound", err)
	}
}

func TestStepRuns_FanoutFields(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	prID := seedStepRunFixtures(t, database)
	stepRuns := NewStepRuns(database)

	sr := newTestStepRun(prID, "review-each")
	sr.IndexInFanout = 2
	sr.Item = `{"path":"main.go"}`
	if err := stepRuns.Create(ctx, sr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := stepRuns.Get(ctx, sr.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IndexInFanout != 2 || got.Item != `{"path":"main.go"}` {
		t.Fatalf("Get = %+v, want fanout fields preserved", got)
	}
}
