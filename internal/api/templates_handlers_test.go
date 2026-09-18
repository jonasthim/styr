package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

type templateOut struct {
	ID             string  `json:"id"`
	OwnerID        *string `json:"owner_id"`
	Name           string  `json:"name"`
	WorkspaceID    string  `json:"workspace_id"`
	ProfileID      string  `json:"profile_id"`
	TitleTemplate  string  `json:"title_template"`
	PromptTemplate string  `json:"prompt_template"`
	SystemPrompt   string  `json:"system_prompt"`
	ReportSchema   string  `json:"report_schema"`
	LoopUntil      string  `json:"loop_until"`
	LoopMax        int     `json:"loop_max"`
}

func sampleTemplate() domain.Template {
	now := time.Now()
	return domain.Template{
		ID: "tmpl-1", Name: "grafana", WorkspaceID: "ws1", ProfileID: "investigate",
		TitleTemplate: "{{ .status }}", PromptTemplate: "Investigate {{ .status }}",
		SystemPrompt: "be careful", ReportSchema: "{}", CreatedAt: now, UpdatedAt: now,
	}
}

func TestTemplatesCreate_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.CreateTemplateFn = func(_ context.Context, _ api.Actor, in domain.TemplateInput) (domain.Template, error) {
		tmpl := sampleTemplate()
		tmpl.Name = in.Name
		return tmpl, nil
	}

	var out templateOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates", map[string]any{
		"name": "grafana", "workspace_id": "ws1", "profile_id": "investigate",
		"prompt_template": "hi {{ .status }}",
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /templates = %d, want 201", status)
	}
	if out.Name != "grafana" || out.ID != "tmpl-1" {
		t.Fatalf("out = %+v", out)
	}

	call, ok := e.triggers.lastCall()
	if !ok || call.method != "CreateTemplate" {
		t.Fatalf("last call = %+v, ok=%v, want CreateTemplate", call, ok)
	}
	in, ok := call.args[0].(domain.TemplateInput)
	if !ok || in.Name != "grafana" || in.WorkspaceID != "ws1" || in.ProfileID != "investigate" {
		t.Fatalf("CreateTemplate input = %+v", call.args[0])
	}
}

func TestTemplatesList_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.ListTemplatesFn = func(context.Context, api.Actor) ([]domain.Template, error) {
		return []domain.Template{sampleTemplate()}, nil
	}

	var out []templateOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/templates", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /templates = %d, want 200", status)
	}
	if len(out) != 1 || out[0].ID != "tmpl-1" {
		t.Fatalf("out = %+v", out)
	}
}

func TestTemplatesGet_NotFound(t *testing.T) {
	e := newEnv(t)
	// e.triggers.GetTemplateFn left unset: the fake's default returns
	// domain.ErrNotFound, exercising WriteError's mapping.
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/templates/missing", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing template = %d, want 404", status)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("error code = %q, want not_found", body.Error.Code)
	}
}

func TestTemplatesPatch_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.UpdateTemplateFn = func(_ context.Context, _ api.Actor, id string, in domain.TemplateInput) (domain.Template, error) {
		tmpl := sampleTemplate()
		tmpl.ID = id
		tmpl.SystemPrompt = in.SystemPrompt
		return tmpl, nil
	}

	var out templateOut
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/templates/tmpl-1", map[string]any{
		"name": "grafana", "workspace_id": "ws1", "profile_id": "investigate",
		"prompt_template": "p", "system_prompt": "new prompt",
	}, &out)
	if status != http.StatusOK {
		t.Fatalf("PATCH /templates/tmpl-1 = %d, want 200", status)
	}
	if out.SystemPrompt != "new prompt" {
		t.Fatalf("system_prompt = %q, want %q", out.SystemPrompt, "new prompt")
	}
}

func TestTemplatesDelete_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.DeleteTemplateFn = func(context.Context, api.Actor, string) error { return nil }

	status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/templates/tmpl-1", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /templates/tmpl-1 = %d, want 204", status)
	}
	call, ok := e.triggers.lastCall()
	if !ok || call.method != "DeleteTemplate" || call.args[0] != "tmpl-1" {
		t.Fatalf("last call = %+v, ok=%v", call, ok)
	}
}

func TestTemplatesRender_DefaultsKindToGrafana(t *testing.T) {
	e := newEnv(t)
	var gotKind string
	var gotPayload []byte
	e.triggers.RenderTemplateFn = func(_ context.Context, _ api.Actor, id, kind string, payload []byte) (domain.RenderResult, error) {
		gotKind = kind
		gotPayload = payload
		return domain.RenderResult{Title: "T", Prompt: "P", Errors: nil}, nil
	}

	var out struct {
		Title  string   `json:"title"`
		Prompt string   `json:"prompt"`
		Errors []string `json:"errors"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates/tmpl-1/render",
		map[string]any{"payload": map[string]any{"status": "firing"}}, &out)
	if status != http.StatusOK {
		t.Fatalf("POST render = %d, want 200", status)
	}
	if out.Title != "T" || out.Prompt != "P" {
		t.Fatalf("out = %+v", out)
	}
	if out.Errors == nil {
		t.Fatalf("errors = nil, want an empty (not null) array")
	}
	if gotKind != "grafana" {
		t.Fatalf("kind passed to RenderTemplate = %q, want grafana default", gotKind)
	}
	if string(gotPayload) != `{"status":"firing"}` {
		t.Fatalf("payload passed to RenderTemplate = %q", gotPayload)
	}
}

func TestTemplatesRender_PassesExplicitKind(t *testing.T) {
	e := newEnv(t)
	var gotKind string
	e.triggers.RenderTemplateFn = func(_ context.Context, _ api.Actor, id, kind string, payload []byte) (domain.RenderResult, error) {
		gotKind = kind
		return domain.RenderResult{}, nil
	}

	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates/tmpl-1/render",
		map[string]any{"payload": map[string]any{}, "kind": "grafana"}, nil)
	if status != http.StatusOK {
		t.Fatalf("POST render = %d, want 200", status)
	}
	if gotKind != "grafana" {
		t.Fatalf("kind = %q, want grafana", gotKind)
	}
}

func TestTemplatesRender_OverrideRendersLocallyWithoutCallingRenderTemplate(t *testing.T) {
	e := newEnv(t)
	e.triggers.GetTemplateFn = func(_ context.Context, _ api.Actor, id string) (domain.Template, error) {
		tmpl := sampleTemplate()
		tmpl.ID = id
		tmpl.TitleTemplate = "stored title {{ .status }}"
		return tmpl, nil
	}
	e.triggers.RenderTemplateFn = func(context.Context, api.Actor, string, string, []byte) (domain.RenderResult, error) {
		t.Fatal("RenderTemplate must not be called when an override is given")
		return domain.RenderResult{}, nil
	}

	var out struct {
		Title  string   `json:"title"`
		Prompt string   `json:"prompt"`
		Errors []string `json:"errors"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates/tmpl-1/render", map[string]any{
		"payload":         map[string]any{"status": "firing"},
		"prompt_template": "override prompt: {{ .status }}",
	}, &out)
	if status != http.StatusOK {
		t.Fatalf("POST render = %d, want 200", status)
	}
	// title_template was not overridden, so it falls back to the stored
	// template's own title_template, rendered against the same payload.
	if out.Title != "stored title firing" {
		t.Fatalf("title = %q, want the stored template's title rendered", out.Title)
	}
	if out.Prompt != "override prompt: firing" {
		t.Fatalf("prompt = %q, want the override rendered", out.Prompt)
	}
}

func TestTemplatesRender_OverrideWithMissingTemplateIs404(t *testing.T) {
	e := newEnv(t)
	// GetTemplateFn left unset: the fake's default returns domain.ErrNotFound.
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates/missing/render", map[string]any{
		"payload": map[string]any{"status": "firing"}, "title_template": "x",
	}, &body)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestTemplatesRender_ErrorsSurfaceIn422(t *testing.T) {
	e := newEnv(t)
	e.triggers.RenderTemplateFn = func(context.Context, api.Actor, string, string, []byte) (domain.RenderResult, error) {
		return domain.RenderResult{}, domain.ErrInvalid
	}

	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates/tmpl-1/render",
		map[string]any{"payload": map[string]any{}}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST render with a bad template = %d, want 422", status)
	}
}

// TestTemplatesCreateAndPatch_LoopFieldsRoundTrip checks that loop_until
// and loop_max survive both create and patch, in both directions (request
// body -> domain.TemplateInput -> response JSON).
func TestTemplatesCreateAndPatch_LoopFieldsRoundTrip(t *testing.T) {
	e := newEnv(t)
	var gotCreateIn domain.TemplateInput
	e.triggers.CreateTemplateFn = func(_ context.Context, _ api.Actor, in domain.TemplateInput) (domain.Template, error) {
		gotCreateIn = in
		tmpl := sampleTemplate()
		tmpl.LoopUntil = in.LoopUntil
		tmpl.LoopMax = in.LoopMax
		return tmpl, nil
	}

	var out templateOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates", map[string]any{
		"name": "grafana", "workspace_id": "ws1", "profile_id": "investigate",
		"prompt_template": "hi", "loop_until": "done", "loop_max": 5,
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /templates = %d, want 201", status)
	}
	if gotCreateIn.LoopUntil != "done" || gotCreateIn.LoopMax != 5 {
		t.Fatalf("CreateTemplate input = %+v, want loop_until=done loop_max=5", gotCreateIn)
	}
	if out.LoopUntil != "done" || out.LoopMax != 5 {
		t.Fatalf("out = %+v, want loop_until=done loop_max=5", out)
	}

	var gotPatchIn domain.TemplateInput
	e.triggers.UpdateTemplateFn = func(_ context.Context, _ api.Actor, id string, in domain.TemplateInput) (domain.Template, error) {
		gotPatchIn = in
		tmpl := sampleTemplate()
		tmpl.ID = id
		tmpl.LoopUntil = in.LoopUntil
		tmpl.LoopMax = in.LoopMax
		return tmpl, nil
	}
	var patchOut templateOut
	status = e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/templates/tmpl-1", map[string]any{
		"name": "grafana", "workspace_id": "ws1", "profile_id": "investigate",
		"prompt_template": "hi", "loop_until": "resolved", "loop_max": 3,
	}, &patchOut)
	if status != http.StatusOK {
		t.Fatalf("PATCH /templates/tmpl-1 = %d, want 200", status)
	}
	if gotPatchIn.LoopUntil != "resolved" || gotPatchIn.LoopMax != 3 {
		t.Fatalf("UpdateTemplate input = %+v, want loop_until=resolved loop_max=3", gotPatchIn)
	}
	if patchOut.LoopUntil != "resolved" || patchOut.LoopMax != 3 {
		t.Fatalf("patchOut = %+v, want loop_until=resolved loop_max=3", patchOut)
	}
}

func TestTemplatesRun_Returns202WithRunID(t *testing.T) {
	e := newEnv(t)
	var gotID string
	var gotVars templates.Vars
	e.runs.StartManualFn = func(_ context.Context, _ api.Actor, templateID string, vars templates.Vars) (domain.Run, error) {
		gotID, gotVars = templateID, vars
		return domain.Run{ID: "run-1"}, nil
	}

	var out runIDOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates/tmpl-1/run",
		map[string]any{"vars": map[string]any{"a": 1}}, &out)
	if status != http.StatusAccepted {
		t.Fatalf("POST /templates/tmpl-1/run = %d, want 202", status)
	}
	if out.RunID != "run-1" {
		t.Fatalf("run_id = %q, want run-1", out.RunID)
	}
	if gotID != "tmpl-1" {
		t.Fatalf("template id passed to StartManual = %q, want tmpl-1", gotID)
	}
	if gotVars["a"] != float64(1) {
		t.Fatalf("vars passed to StartManual = %+v", gotVars)
	}
}

// TestTemplatesRun_NoBodyMeansNoVars checks that an entirely empty request
// body (vars is documented as optional) is accepted, not a 422.
func TestTemplatesRun_NoBodyMeansNoVars(t *testing.T) {
	e := newEnv(t)
	var gotVars templates.Vars
	e.runs.StartManualFn = func(_ context.Context, _ api.Actor, _ string, vars templates.Vars) (domain.Run, error) {
		gotVars = vars
		return domain.Run{ID: "run-1"}, nil
	}

	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/templates/tmpl-1/run", nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("POST /templates/tmpl-1/run (no body) = %d, want 202", status)
	}
	if len(gotVars) != 0 {
		t.Fatalf("vars = %+v, want empty", gotVars)
	}
}
