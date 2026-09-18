package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/pipelines"
)

type pipelineRunOut struct {
	ID         string          `json:"id"`
	PipelineID string          `json:"pipeline_id"`
	Origin     string          `json:"origin"`
	OriginRef  string          `json:"origin_ref"`
	Input      json.RawMessage `json:"input"`
	State      string          `json:"state"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	CostUSD    float64         `json:"cost_usd"`
}

type stepRunOut struct {
	ID            string          `json:"id"`
	PipelineRunID string          `json:"pipeline_run_id"`
	StepID        string          `json:"step_id"`
	IndexInFanout int             `json:"index_in_fanout"`
	Item          string          `json:"item"`
	RunID         *string         `json:"run_id"`
	Attempt       int             `json:"attempt"`
	State         string          `json:"state"`
	Report        json.RawMessage `json:"report"`
	StartedAt     *time.Time      `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
	Worktree      string          `json:"worktree"`
	Run           *runOut         `json:"run"`
}

type pipelineRunViewOut struct {
	Run      pipelineRunOut `json:"run"`
	Pipeline pipelineOut    `json:"pipeline"`
	Steps    []stepRunOut   `json:"steps"`
	Graph    graphOut       `json:"graph"`
}

func samplePipelineRun() domain.PipelineRun {
	now := time.Now()
	return domain.PipelineRun{
		ID: "prun-1", PipelineID: "pipe-1", Origin: "ui", Input: json.RawMessage(`{}`),
		State: domain.PipelineRunRunning, StartedAt: now,
	}
}

func TestPipelineRunsList_PassesFiltersThrough(t *testing.T) {
	e := newEnv(t)
	e.pipelines.ListRunsFn = func(context.Context, api.Actor, domain.PipelineRunFilter) ([]domain.PipelineRun, error) {
		return []domain.PipelineRun{samplePipelineRun()}, nil
	}
	var out []pipelineRunOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/pipeline-runs?pipeline=pipe-1&state=running&limit=10", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /pipeline-runs = %d, want 200", status)
	}
	if len(out) != 1 || out[0].ID != "prun-1" {
		t.Fatalf("out = %+v", out)
	}
	call, ok := e.pipelines.lastCall()
	if !ok || call.method != "ListRuns" {
		t.Fatalf("last call = %+v, ok=%v", call, ok)
	}
	filter, ok := call.args[0].(domain.PipelineRunFilter)
	if !ok {
		t.Fatalf("call.args[0] type = %T", call.args[0])
	}
	if filter.PipelineID != "pipe-1" || filter.State != "running" || filter.Limit != 10 {
		t.Fatalf("filter = %+v", filter)
	}
}

func TestPipelineRunsGet_IncludesStepsWithRunSummary(t *testing.T) {
	e := newEnv(t)
	runID := "run-1"
	e.pipelines.GetRunFn = func(_ context.Context, _ api.Actor, id string) (pipelines.RunView, error) {
		pr := samplePipelineRun()
		pr.ID = id
		return pipelines.RunView{
			Run:      pr,
			Pipeline: samplePipeline(),
			Steps: []pipelines.StepView{
				{
					Step: domain.StepRun{
						ID: "step-1", PipelineRunID: id, StepID: "triage", RunID: &runID, Attempt: 1,
						State: domain.StepRunSuccess, Report: json.RawMessage(`{"ok":true}`),
					},
					RunSummary: &domain.Run{ID: runID, Origin: "pipeline", Outcome: domain.RunSuccess},
				},
				{
					Step: domain.StepRun{
						ID: "step-2", PipelineRunID: id, StepID: "fix", Attempt: 1, State: domain.StepRunPending,
					},
					RunSummary: nil,
				},
			},
			Graph: pipelines.Graph{
				Nodes: []pipelines.Node{{ID: "triage"}, {ID: "fix"}},
				Edges: []pipelines.Edge{{From: "triage", To: "fix"}},
			},
		}, nil
	}

	var out pipelineRunViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/pipeline-runs/prun-1", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /pipeline-runs/prun-1 = %d, want 200", status)
	}
	if out.Run.ID != "prun-1" || out.Pipeline.ID != "pipe-1" {
		t.Fatalf("out = %+v", out)
	}
	if len(out.Steps) != 2 {
		t.Fatalf("steps = %+v, want 2", out.Steps)
	}
	if out.Steps[0].Run == nil || out.Steps[0].Run.ID != runID {
		t.Fatalf("steps[0].run = %+v, want run-1", out.Steps[0].Run)
	}
	if string(out.Steps[0].Report) != `{"ok":true}` {
		t.Fatalf("steps[0].report = %s", out.Steps[0].Report)
	}
	if out.Steps[1].Run != nil {
		t.Fatalf("steps[1].run = %+v, want nil", out.Steps[1].Run)
	}
	if string(out.Steps[1].Report) != "null" {
		t.Fatalf("steps[1].report = %s, want the JSON literal null", out.Steps[1].Report)
	}
	if len(out.Graph.Nodes) != 2 || len(out.Graph.Edges) != 1 {
		t.Fatalf("graph = %+v", out.Graph)
	}
}

func TestPipelineRunsGet_NotFound(t *testing.T) {
	e := newEnv(t)
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/pipeline-runs/missing", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing pipeline run = %d, want 404", status)
	}
}

func TestPipelineRunsCancel_Returns202(t *testing.T) {
	e := newEnv(t)
	e.pipelines.CancelFn = func(context.Context, api.Actor, string) error { return nil }
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipeline-runs/prun-1/cancel", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("POST cancel = %d, want 202", status)
	}
}

func TestPipelineRunsCancel_ConflictIs409(t *testing.T) {
	e := newEnv(t)
	e.pipelines.CancelFn = func(context.Context, api.Actor, string) error { return domain.ErrConflict }
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipeline-runs/prun-1/cancel", nil, nil)
	if status != http.StatusConflict {
		t.Fatalf("POST cancel (not running) = %d, want 409", status)
	}
}

func TestPipelineRunsRetryFailed_Returns202(t *testing.T) {
	e := newEnv(t)
	e.pipelines.RetryFailedFn = func(context.Context, api.Actor, string) error { return nil }
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipeline-runs/prun-1/retry-failed", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("POST retry-failed = %d, want 202", status)
	}
}

func TestPipelineRunsRetryFailed_ConflictIs409(t *testing.T) {
	e := newEnv(t)
	e.pipelines.RetryFailedFn = func(context.Context, api.Actor, string) error { return domain.ErrConflict }
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipeline-runs/prun-1/retry-failed", nil, nil)
	if status != http.StatusConflict {
		t.Fatalf("POST retry-failed (nothing failed) = %d, want 409", status)
	}
}
