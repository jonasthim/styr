package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/runs"
)

type loopOut struct {
	ID            string  `json:"id"`
	TemplateID    string  `json:"template_id"`
	SessionID     *string `json:"session_id"`
	Origin        string  `json:"origin"`
	OriginRef     string  `json:"origin_ref"`
	UntilField    string  `json:"until_field"`
	MaxIterations int     `json:"max_iterations"`
	Iteration     int     `json:"iteration"`
	State         string  `json:"state"`
}

type loopViewOut struct {
	Loop     loopOut      `json:"loop"`
	Runs     []runOut     `json:"runs"`
	Template *templateOut `json:"template"`
}

func sampleLoop() domain.Loop {
	now := time.Now()
	sess := "sess-1"
	return domain.Loop{
		ID: "loop-1", TemplateID: "tmpl-1", SessionID: &sess, Origin: "ui",
		UntilField: "done", MaxIterations: 5, Iteration: 2, State: domain.LoopRunning,
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestLoopsList_PassesStateAndLimit(t *testing.T) {
	e := newEnv(t)
	e.runs.ListLoopsFn = func(context.Context, string, int) ([]domain.Loop, error) {
		return []domain.Loop{sampleLoop()}, nil
	}

	var out []loopOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/loops?state=running&limit=10", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /loops = %d, want 200", status)
	}
	if len(out) != 1 || out[0].ID != "loop-1" || out[0].State != "running" {
		t.Fatalf("out = %+v", out)
	}

	call, ok := e.runs.lastCall()
	if !ok || call.method != "ListLoops" || call.args[0] != "running" || call.args[1] != 10 {
		t.Fatalf("last call = %+v, ok=%v, want ListLoops(running, 10)", call, ok)
	}
}

func TestLoopsGet_ReturnsLoopRunsAndTemplate(t *testing.T) {
	e := newEnv(t)
	e.runs.GetLoopFn = func(context.Context, string) (runs.LoopView, error) {
		tmpl := sampleTemplate()
		return runs.LoopView{
			Loop: sampleLoop(),
			Runs: []domain.Run{
				{ID: "run-1", SessionID: "sess-1", Outcome: domain.RunSuccess, LoopID: "loop-1", Iteration: 1},
				{ID: "run-2", SessionID: "sess-1", Outcome: domain.RunRunning, LoopID: "loop-1", Iteration: 2},
			},
			Template: &tmpl,
		}, nil
	}

	var out loopViewOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/loops/loop-1", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /loops/loop-1 = %d, want 200", status)
	}
	if out.Loop.ID != "loop-1" {
		t.Fatalf("loop = %+v", out.Loop)
	}
	if len(out.Runs) != 2 || out.Runs[0].ID != "run-1" || out.Runs[1].ID != "run-2" {
		t.Fatalf("runs = %+v", out.Runs)
	}
	if out.Template == nil || out.Template.ID != "tmpl-1" {
		t.Fatalf("template = %+v", out.Template)
	}
}

func TestLoopsGet_NotFound(t *testing.T) {
	e := newEnv(t)
	// GetLoopFn left unset: the fake's default returns domain.ErrNotFound.
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/loops/missing", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing loop = %d, want 404", status)
	}
}

func TestLoopsStop_Returns202(t *testing.T) {
	e := newEnv(t)
	e.runs.StopFn = func(context.Context, api.Actor, string) error { return nil }
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/loops/loop-1/stop", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("POST /loops/loop-1/stop = %d, want 202", status)
	}
}

func TestLoopsStop_NotRunningIs409(t *testing.T) {
	e := newEnv(t)
	e.runs.StopFn = func(context.Context, api.Actor, string) error {
		return domain.ErrConflict
	}
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/loops/loop-1/stop", nil, &body)
	if status != http.StatusConflict {
		t.Fatalf("POST stop on a non-running loop = %d, want 409", status)
	}
	if body.Error.Code != "conflict" {
		t.Fatalf("error code = %q, want conflict", body.Error.Code)
	}
}
