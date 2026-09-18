package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/pipelines"
	"github.com/jonasthim/styr/internal/templates"
)

type pipelineOut struct {
	ID          string    `json:"id"`
	OwnerID     *string   `json:"owner_id"`
	Name        string    `json:"name"`
	WorkspaceID string    `json:"workspace_id"`
	YAML        string    `json:"yaml"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type problemOut struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type graphOut struct {
	Nodes []struct {
		ID       string `json:"id"`
		Template string `json:"template"`
		Worktree string `json:"worktree"`
		Foreach  bool   `json:"foreach"`
	} `json:"nodes"`
	Edges []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"edges"`
}

type pipelineErrorOut struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Errors []problemOut `json:"errors"`
}

type validationResultOut struct {
	OK     bool         `json:"ok"`
	Errors []problemOut `json:"errors"`
	Graph  graphOut     `json:"graph"`
}

func samplePipeline() domain.Pipeline {
	now := time.Now()
	return domain.Pipeline{
		ID: "pipe-1", Name: "fix-ci", WorkspaceID: "ws-1", YAML: "name: fix-ci\nworkspace: styr\nsteps: []\n",
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestPipelinesList_OK(t *testing.T) {
	e := newEnv(t)
	e.pipelines.ListPipelinesFn = func(context.Context, api.Actor) ([]domain.Pipeline, error) {
		return []domain.Pipeline{samplePipeline()}, nil
	}
	var out []pipelineOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/pipelines", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /pipelines = %d, want 200", status)
	}
	if len(out) != 1 || out[0].ID != "pipe-1" {
		t.Fatalf("out = %+v", out)
	}
}

func TestPipelinesCreate_OK(t *testing.T) {
	e := newEnv(t)
	e.pipelines.CreatePipelineFn = func(_ context.Context, _ api.Actor, in domain.PipelineInput) (domain.Pipeline, error) {
		p := samplePipeline()
		p.Name = in.Name
		p.WorkspaceID = in.WorkspaceID
		p.YAML = in.YAML
		return p, nil
	}
	var out pipelineOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipelines", map[string]any{
		"name": "fix-ci", "workspace_id": "ws-1", "yaml": "name: fix-ci\n",
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /pipelines = %d, want 201", status)
	}
	if out.ID != "pipe-1" || out.Name != "fix-ci" {
		t.Fatalf("out = %+v", out)
	}
}

func TestPipelinesCreate_ValidationErrorIs422WithProblems(t *testing.T) {
	e := newEnv(t)
	e.pipelines.CreatePipelineFn = func(context.Context, api.Actor, domain.PipelineInput) (domain.Pipeline, error) {
		return domain.Pipeline{}, &pipelines.ValidationError{Problems: []pipelines.Problem{
			{Line: 3, Message: `step "fix": template "missing" not found`},
			{Line: 5, Message: `step "verify" needs unknown step "nope"`},
		}}
	}
	var out pipelineErrorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipelines", map[string]any{
		"name": "fix-ci", "workspace_id": "ws-1", "yaml": "name: fix-ci\n",
	}, &out)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if out.Error.Code != "invalid_pipeline" {
		t.Fatalf("error code = %q, want invalid_pipeline", out.Error.Code)
	}
	if len(out.Errors) != 2 {
		t.Fatalf("errors = %+v, want 2 problems", out.Errors)
	}
	if out.Errors[0].Line != 3 || out.Errors[1].Line != 5 {
		t.Fatalf("errors = %+v, want lines 3 and 5", out.Errors)
	}
}

func TestPipelinesGet_OK(t *testing.T) {
	e := newEnv(t)
	e.pipelines.GetPipelineFn = func(_ context.Context, _ api.Actor, id string) (domain.Pipeline, error) {
		p := samplePipeline()
		p.ID = id
		return p, nil
	}
	var out pipelineOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/pipelines/pipe-1", nil, &out)
	if status != http.StatusOK || out.ID != "pipe-1" {
		t.Fatalf("GET /pipelines/pipe-1 = %d, out = %+v", status, out)
	}
}

func TestPipelinesGet_NotFound(t *testing.T) {
	e := newEnv(t)
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/pipelines/missing", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing pipeline = %d, want 404", status)
	}
}

func TestPipelinesPatch_OK(t *testing.T) {
	e := newEnv(t)
	e.pipelines.UpdatePipelineFn = func(_ context.Context, _ api.Actor, id string, in domain.PipelineInput) (domain.Pipeline, error) {
		p := samplePipeline()
		p.ID = id
		p.YAML = in.YAML
		return p, nil
	}
	var out pipelineOut
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/pipelines/pipe-1", map[string]any{
		"name": "fix-ci", "workspace_id": "ws-1", "yaml": "name: fix-ci\nsteps: []\n",
	}, &out)
	if status != http.StatusOK || out.YAML != "name: fix-ci\nsteps: []\n" {
		t.Fatalf("PATCH /pipelines/pipe-1 = %d, out = %+v", status, out)
	}
}

func TestPipelinesPatch_ValidationErrorIs422(t *testing.T) {
	e := newEnv(t)
	e.pipelines.UpdatePipelineFn = func(context.Context, api.Actor, string, domain.PipelineInput) (domain.Pipeline, error) {
		return domain.Pipeline{}, &pipelines.ValidationError{Problems: []pipelines.Problem{{Line: 1, Message: "bad"}}}
	}
	var out pipelineErrorOut
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/pipelines/pipe-1", map[string]any{
		"name": "fix-ci", "workspace_id": "ws-1", "yaml": "bogus",
	}, &out)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if out.Error.Code != "invalid_pipeline" || len(out.Errors) != 1 {
		t.Fatalf("out = %+v", out)
	}
}

func TestPipelinesDelete_OK(t *testing.T) {
	e := newEnv(t)
	e.pipelines.DeletePipelineFn = func(context.Context, api.Actor, string) error { return nil }
	status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/pipelines/pipe-1", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /pipelines/pipe-1 = %d, want 204", status)
	}
}

func TestPipelinesValidate_OK(t *testing.T) {
	e := newEnv(t)
	e.pipelines.ValidateFn = func(_ context.Context, _ api.Actor, workspaceID string, yamlText []byte) (pipelines.ValidationResult, error) {
		if workspaceID != "ws-1" {
			t.Fatalf("workspace_id = %q, want ws-1", workspaceID)
		}
		if string(yamlText) != "name: fix-ci\n" {
			t.Fatalf("yaml = %q", yamlText)
		}
		return pipelines.ValidationResult{
			OK: true,
			Graph: pipelines.Graph{
				Nodes: []pipelines.Node{{ID: "triage", Template: "CI failure triage", Worktree: "own"}},
				Edges: nil,
			},
		}, nil
	}
	var out validationResultOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipelines/validate", map[string]any{
		"yaml": "name: fix-ci\n", "workspace_id": "ws-1",
	}, &out)
	if status != http.StatusOK {
		t.Fatalf("POST /pipelines/validate = %d, want 200", status)
	}
	if !out.OK {
		t.Fatalf("ok = false, want true")
	}
	if len(out.Graph.Nodes) != 1 || out.Graph.Nodes[0].ID != "triage" {
		t.Fatalf("graph = %+v", out.Graph)
	}
}

func TestPipelinesValidate_InvalidReturns200WithErrors(t *testing.T) {
	e := newEnv(t)
	e.pipelines.ValidateFn = func(context.Context, api.Actor, string, []byte) (pipelines.ValidationResult, error) {
		return pipelines.ValidationResult{
			OK:       false,
			Problems: []pipelines.Problem{{Line: 2, Message: "step \"fix\": template \"nope\" not found"}},
			Graph:    pipelines.Graph{Nodes: []pipelines.Node{{ID: "fix"}}},
		}, nil
	}
	var out validationResultOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipelines/validate", map[string]any{
		"yaml": "bogus", "workspace_id": "ws-1",
	}, &out)
	if status != http.StatusOK {
		t.Fatalf("POST /pipelines/validate (invalid) = %d, want 200", status)
	}
	if out.OK {
		t.Fatalf("ok = true, want false")
	}
	if len(out.Errors) != 1 || out.Errors[0].Line != 2 {
		t.Fatalf("errors = %+v", out.Errors)
	}
	if len(out.Graph.Nodes) != 1 {
		t.Fatalf("graph = %+v, want the (partial) graph rendered anyway", out.Graph)
	}
}

func TestPipelinesStart_Returns202WithPipelineRunID(t *testing.T) {
	e := newEnv(t)
	var gotOrigin domain.Origin
	var gotInput templates.Vars
	e.pipelines.StartFn = func(_ context.Context, _ api.Actor, pipelineID string, input templates.Vars, origin domain.Origin, originRef string) (domain.PipelineRun, error) {
		gotOrigin = origin
		gotInput = input
		if pipelineID != "pipe-1" {
			t.Fatalf("pipelineID = %q, want pipe-1", pipelineID)
		}
		if originRef != "" {
			t.Fatalf("originRef = %q, want empty", originRef)
		}
		return domain.PipelineRun{ID: "prun-1", PipelineID: pipelineID}, nil
	}
	var out struct {
		PipelineRunID string `json:"pipeline_run_id"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipelines/pipe-1/start", map[string]any{
		"input": map[string]any{"alert": "disk full"},
	}, &out)
	if status != http.StatusAccepted {
		t.Fatalf("POST /pipelines/pipe-1/start = %d, want 202", status)
	}
	if out.PipelineRunID != "prun-1" {
		t.Fatalf("pipeline_run_id = %q, want prun-1", out.PipelineRunID)
	}
	if gotOrigin != domain.OriginUI {
		t.Fatalf("origin = %q, want ui", gotOrigin)
	}
	if gotInput["alert"] != "disk full" {
		t.Fatalf("input = %+v", gotInput)
	}
}

func TestPipelinesStart_NoBody_OK(t *testing.T) {
	e := newEnv(t)
	e.pipelines.StartFn = func(context.Context, api.Actor, string, templates.Vars, domain.Origin, string) (domain.PipelineRun, error) {
		return domain.PipelineRun{ID: "prun-2"}, nil
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/pipelines/pipe-1/start", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("POST /pipelines/pipe-1/start (no body) = %d, want 202", status)
	}
}
