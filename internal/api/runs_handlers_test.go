package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

type runOut struct {
	ID        string  `json:"id"`
	SessionID string  `json:"session_id"`
	Origin    string  `json:"origin"`
	StepRunID *string `json:"step_run_id"`
	LoopID    string  `json:"loop_id"`
	Iteration int     `json:"iteration"`
	Outcome   string  `json:"outcome"`
	Summary   string  `json:"summary"`
	CostUSD   float64 `json:"cost_usd"`
}

type runViewOut struct {
	Run      runOut       `json:"run"`
	Session  *sessionOut  `json:"session"`
	Delivery *deliveryOut `json:"delivery"`
	Template *templateOut `json:"template"`
	Loop     *loopOut     `json:"loop"`
	Step     *stepRunOut  `json:"step"`
	Pipeline *pipelineOut `json:"pipeline"`
}

// sessionOut mirrors the fields runs_handlers_test.go asserts on; the full
// session shape is already covered by sessions_handlers_test.go.
type sessionOut struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func sampleRunView() domain.RunView {
	now := time.Now()
	return domain.RunView{
		Run: domain.Run{
			ID: "run-1", SessionID: "sess-1", Origin: "webhook",
			StartedAt: now, Outcome: domain.RunSuccess, Summary: "all good", CostUSD: 0.42,
		},
		Session: &domain.Session{ID: "sess-1", Title: "investigate alert"},
	}
}

func TestRunsList_PassesFiltersThrough(t *testing.T) {
	e := newEnv(t)
	e.runs.ListFn = func(context.Context, domain.RunFilter) ([]domain.RunView, error) {
		return []domain.RunView{sampleRunView()}, nil
	}

	var out []runViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs?outcome=failed&trigger=trig-1&limit=25", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /runs = %d, want 200", status)
	}
	if len(out) != 1 || out[0].Run.ID != "run-1" {
		t.Fatalf("out = %+v", out)
	}
	if out[0].Session == nil || out[0].Session.ID != "sess-1" {
		t.Fatalf("session = %+v, want embedded session", out[0].Session)
	}

	call, ok := e.runs.lastCall()
	if !ok || call.method != "List" {
		t.Fatalf("last call = %+v, ok=%v", call, ok)
	}
	filter, ok := call.args[0].(domain.RunFilter)
	if !ok {
		t.Fatalf("call.args[0] type = %T", call.args[0])
	}
	if filter.Outcome != "failed" || filter.TriggerID != "trig-1" || filter.Limit != 25 {
		t.Fatalf("filter = %+v, want outcome=failed trigger=trig-1 limit=25", filter)
	}
}

func TestRunsList_NoQueryLeavesFilterZero(t *testing.T) {
	e := newEnv(t)
	e.runs.ListFn = func(context.Context, domain.RunFilter) ([]domain.RunView, error) { return nil, nil }

	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs", nil, nil); status != http.StatusOK {
		t.Fatalf("GET /runs = %d, want 200", status)
	}
	call, _ := e.runs.lastCall()
	filter := call.args[0].(domain.RunFilter)
	if filter.Outcome != "" || filter.TriggerID != "" || filter.Limit != 0 {
		t.Fatalf("filter = %+v, want zero value", filter)
	}
}

func TestRunsGet_OK(t *testing.T) {
	e := newEnv(t)
	e.runs.GetFn = func(_ context.Context, id string) (domain.RunView, error) {
		v := sampleRunView()
		v.Run.ID = id
		return v, nil
	}
	var out runViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs/run-1", nil, &out)
	if status != http.StatusOK || out.Run.ID != "run-1" {
		t.Fatalf("GET /runs/run-1 = %d, out = %+v", status, out)
	}
}

func TestRunsGet_IncludesStepRunID(t *testing.T) {
	e := newEnv(t)
	stepRunID := "step-1"
	e.runs.GetFn = func(context.Context, string) (domain.RunView, error) {
		return domain.RunView{Run: domain.Run{
			ID: "run-3", Origin: "pipeline", StepRunID: &stepRunID, Outcome: domain.RunSuccess,
		}}, nil
	}
	var out runViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs/run-3", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /runs/run-3 = %d, want 200", status)
	}
	if out.Run.StepRunID == nil || *out.Run.StepRunID != "step-1" {
		t.Fatalf("run.step_run_id = %v, want step-1", out.Run.StepRunID)
	}
}

// A run a pipeline step started carries the step and the pipeline it
// belongs to, so the run page can say which graph it is part of and link
// back to it without a second round trip.
func TestRunsGet_IncludesStepAndPipeline(t *testing.T) {
	e := newEnv(t)
	stepRunID := "step-1"
	e.runs.GetFn = func(context.Context, string) (domain.RunView, error) {
		return domain.RunView{
			Run:      domain.Run{ID: "run-4", Origin: "pipeline", StepRunID: &stepRunID, Outcome: domain.RunSuccess},
			Step:     &domain.StepRun{ID: stepRunID, PipelineRunID: "prun-1", StepID: "triage", Attempt: 1, State: domain.StepRunSuccess},
			Pipeline: &domain.Pipeline{ID: "pl-1", Name: "fix-ci", WorkspaceID: "ws-1"},
		}, nil
	}
	var out runViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs/run-4", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /runs/run-4 = %d, want 200", status)
	}
	if out.Step == nil || out.Step.PipelineRunID != "prun-1" || out.Step.StepID != "triage" {
		t.Fatalf("step = %+v, want the pipeline step run", out.Step)
	}
	if out.Pipeline == nil || out.Pipeline.Name != "fix-ci" {
		t.Fatalf("pipeline = %+v, want fix-ci", out.Pipeline)
	}
}

// Every other run has both fields explicitly null rather than missing.
func TestRunsGet_NoStepOutsideAPipeline(t *testing.T) {
	e := newEnv(t)
	e.runs.GetFn = func(context.Context, string) (domain.RunView, error) { return sampleRunView(), nil }
	var out runViewOut
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs/run-1", nil, &out); status != http.StatusOK {
		t.Fatalf("GET /runs/run-1 = %d, want 200", status)
	}
	if out.Step != nil || out.Pipeline != nil {
		t.Fatalf("step = %+v, pipeline = %+v, want both null", out.Step, out.Pipeline)
	}
}

func TestRunsGet_NotFound(t *testing.T) {
	e := newEnv(t)
	// GetFn left unset: the fake's default returns domain.ErrNotFound.
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs/missing", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing run = %d, want 404", status)
	}
}

func TestRunsGet_NilSessionOmittedGracefully(t *testing.T) {
	e := newEnv(t)
	e.runs.GetFn = func(context.Context, string) (domain.RunView, error) {
		return domain.RunView{Run: domain.Run{ID: "run-2", Outcome: domain.RunRunning}}, nil
	}
	var out runViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs/run-2", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /runs/run-2 = %d, want 200", status)
	}
	if out.Session != nil || out.Delivery != nil || out.Template != nil || out.Loop != nil {
		t.Fatalf("out = %+v, want nil session/delivery/template/loop", out)
	}
}

func TestRunsList_PassesLoopIDFilterThrough(t *testing.T) {
	e := newEnv(t)
	e.runs.ListFn = func(context.Context, domain.RunFilter) ([]domain.RunView, error) { return nil, nil }

	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs?loop_id=loop-1", nil, nil); status != http.StatusOK {
		t.Fatalf("GET /runs?loop_id=loop-1 = %d, want 200", status)
	}
	call, ok := e.runs.lastCall()
	if !ok || call.method != "List" {
		t.Fatalf("last call = %+v, ok=%v", call, ok)
	}
	filter := call.args[0].(domain.RunFilter)
	if filter.LoopID != "loop-1" {
		t.Fatalf("filter.LoopID = %q, want loop-1", filter.LoopID)
	}
}

func TestRunsGet_IncludesLoop(t *testing.T) {
	e := newEnv(t)
	sess := "sess-1"
	e.runs.GetFn = func(context.Context, string) (domain.RunView, error) {
		return domain.RunView{
			Run:  domain.Run{ID: "run-3", SessionID: "sess-1", LoopID: "loop-1", Iteration: 2, Outcome: domain.RunRunning},
			Loop: &domain.Loop{ID: "loop-1", TemplateID: "tmpl-1", SessionID: &sess, State: domain.LoopRunning, Iteration: 2, MaxIterations: 5},
		}, nil
	}
	var out runViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/runs/run-3", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /runs/run-3 = %d, want 200", status)
	}
	if out.Run.LoopID != "loop-1" || out.Run.Iteration != 2 {
		t.Fatalf("run = %+v, want loop_id=loop-1 iteration=2", out.Run)
	}
	if out.Loop == nil || out.Loop.ID != "loop-1" || out.Loop.State != "running" {
		t.Fatalf("loop = %+v", out.Loop)
	}
}
