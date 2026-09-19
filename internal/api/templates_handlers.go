package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

// registerTemplatesRoutes mounts template CRUD and dry-run render routes.
// Visibility (owner, shared, or admin) and the "Shared requires admin" rule
// are enforced by TriggersService, not here.
func registerTemplatesRoutes(r chi.Router, d *Deps) {
	r.Get("/templates", handleTemplatesList(d))
	r.Post("/templates", handleTemplatesCreate(d))
	r.Get("/templates/{id}", handleTemplatesGet(d))
	r.Patch("/templates/{id}", handleTemplatesPatch(d))
	r.Delete("/templates/{id}", handleTemplatesDelete(d))
	r.Post("/templates/{id}/render", handleTemplatesRender(d))
	r.Post("/templates/{id}/run", handleTemplatesRun(d))
}

type templateDTO struct {
	ID             string    `json:"id"`
	OwnerID        *string   `json:"owner_id"`
	Name           string    `json:"name"`
	WorkspaceID    string    `json:"workspace_id"`
	ProfileID      string    `json:"profile_id"`
	TitleTemplate  string    `json:"title_template"`
	PromptTemplate string    `json:"prompt_template"`
	SystemPrompt   string    `json:"system_prompt"`
	ReportSchema   string    `json:"report_schema"`
	LoopUntil      string    `json:"loop_until"`
	LoopMax        int       `json:"loop_max"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func templateDTOFrom(t domain.Template) templateDTO {
	return templateDTO{
		ID: t.ID, OwnerID: t.OwnerID, Name: t.Name, WorkspaceID: t.WorkspaceID, ProfileID: t.ProfileID,
		TitleTemplate: t.TitleTemplate, PromptTemplate: t.PromptTemplate, SystemPrompt: t.SystemPrompt,
		ReportSchema: t.ReportSchema, LoopUntil: t.LoopUntil, LoopMax: t.LoopMax,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

type templateInput struct {
	Name           string `json:"name"`
	WorkspaceID    string `json:"workspace_id"`
	ProfileID      string `json:"profile_id"`
	TitleTemplate  string `json:"title_template"`
	PromptTemplate string `json:"prompt_template"`
	SystemPrompt   string `json:"system_prompt"`
	ReportSchema   string `json:"report_schema"`
	LoopUntil      string `json:"loop_until"`
	LoopMax        int    `json:"loop_max"`
	Shared         bool   `json:"shared"`
}

func (in templateInput) toDomain() domain.TemplateInput {
	return domain.TemplateInput{
		Name: in.Name, WorkspaceID: in.WorkspaceID, ProfileID: in.ProfileID,
		TitleTemplate: in.TitleTemplate, PromptTemplate: in.PromptTemplate,
		SystemPrompt: in.SystemPrompt, ReportSchema: in.ReportSchema,
		LoopUntil: in.LoopUntil, LoopMax: in.LoopMax, Shared: in.Shared,
	}
}

// handleTemplatesList is GET /api/v1/templates.
func handleTemplatesList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Triggers.ListTemplates(r.Context(), actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]templateDTO, 0, len(list))
		for _, t := range list {
			out = append(out, templateDTOFrom(t))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleTemplatesCreate is POST /api/v1/templates.
func handleTemplatesCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in templateInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		t, err := d.Triggers.CreateTemplate(r.Context(), actorFrom(r), in.toDomain())
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, templateDTOFrom(t))
	}
}

// handleTemplatesGet is GET /api/v1/templates/{id}.
func handleTemplatesGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, err := d.Triggers.GetTemplate(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, templateDTOFrom(t))
	}
}

// handleTemplatesPatch is PATCH /api/v1/templates/{id}.
func handleTemplatesPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in templateInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		t, err := d.Triggers.UpdateTemplate(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.toDomain())
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, templateDTOFrom(t))
	}
}

// handleTemplatesDelete is DELETE /api/v1/templates/{id}.
func handleTemplatesDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Triggers.DeleteTemplate(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type templateRenderInput struct {
	Payload        json.RawMessage `json:"payload"`
	Kind           string          `json:"kind"`
	TitleTemplate  *string         `json:"title_template"`
	PromptTemplate *string         `json:"prompt_template"`
}

type templateRenderDTO struct {
	Title  string   `json:"title"`
	Prompt string   `json:"prompt"`
	Errors []string `json:"errors"`
}

func templateRenderDTOFrom(result domain.RenderResult) templateRenderDTO {
	errs := result.Errors
	if errs == nil {
		errs = []string{}
	}
	return templateRenderDTO{Title: result.Title, Prompt: result.Prompt, Errors: errs}
}

// handleTemplatesRender is POST /api/v1/templates/{id}/render: a dry run
// that renders a title and prompt against payload without starting a
// session. Kind selects how payload is normalized into template variables
// (see internal/templates.Normalize); it defaults to "grafana" when
// omitted, matching the seeded default template kind. When title_template
// and/or prompt_template are given, they override the stored template's
// own text for this render only (e.g. the UI's live preview while
// editing) — rendered here directly with internal/templates, since
// TriggersService.RenderTemplate always renders the stored template.
func handleTemplatesRender(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in templateRenderInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		kind := in.Kind
		if kind == "" {
			kind = "grafana"
		}
		actor := actorFrom(r)
		id := chi.URLParam(r, "id")

		if in.TitleTemplate != nil || in.PromptTemplate != nil {
			result, err := renderTemplateWithOverrides(r.Context(), d, actor, id, kind, in)
			if err != nil {
				WriteError(w, err)
				return
			}
			WriteJSON(w, http.StatusOK, templateRenderDTOFrom(result))
			return
		}

		result, err := d.Triggers.RenderTemplate(r.Context(), actor, id, kind, in.Payload)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, templateRenderDTOFrom(result))
	}
}

// renderTemplateWithOverrides fetches the stored template (for whichever of
// title/prompt in doesn't override) and renders both locally, collecting
// template-execution errors into RenderResult.Errors rather than failing
// the request — matching RenderTemplate's own documented behaviour.
func renderTemplateWithOverrides(ctx context.Context, d *Deps, actor Actor, id, kind string, in templateRenderInput) (domain.RenderResult, error) {
	tmpl, err := d.Triggers.GetTemplate(ctx, actor, id)
	if err != nil {
		return domain.RenderResult{}, err
	}
	titleTmpl := tmpl.TitleTemplate
	if in.TitleTemplate != nil {
		titleTmpl = *in.TitleTemplate
	}
	promptTmpl := tmpl.PromptTemplate
	if in.PromptTemplate != nil {
		promptTmpl = *in.PromptTemplate
	}

	vars, err := templates.Normalize(kind, in.Payload)
	if err != nil {
		return domain.RenderResult{Errors: []string{err.Error()}}, nil
	}

	var errs []string
	title, err := templates.Render(titleTmpl, vars)
	if err != nil {
		errs = append(errs, err.Error())
	}
	prompt, err := templates.Render(promptTmpl, vars)
	if err != nil {
		errs = append(errs, err.Error())
	}
	return domain.RenderResult{Title: title, Prompt: prompt, Errors: errs}, nil
}

type templateRunInput struct {
	Vars json.RawMessage `json:"vars"`
}

// decodeOptionalJSON decodes r's body into dst when the body carries
// anything, and leaves dst untouched (no error) for a request sent with no
// body at all — POST /templates/{id}/run's whole payload ({vars?}) is
// optional, so an empty body is a valid "no input" request rather than a
// malformed one (unlike decodeJSON, whose callers all require a body).
func decodeOptionalJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// runIDDTO is the {run_id} shape every "start a run" endpoint answers
// with. POST /schedules/{id}/run extends it (see scheduleRunStartedDTO).
type runIDDTO struct {
	RunID string `json:"run_id"`
}

// handleTemplatesRun is POST /api/v1/templates/{id}/run: starts a run of
// the template by hand, as the UI's "run this template now" does — origin
// ui, and a looping template (loop_until set) loops from here exactly as
// it would from a webhook or a schedule. vars is optional; an empty or
// missing body means no vars.
func handleTemplatesRun(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in templateRunInput
		if err := decodeOptionalJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		vars := templates.Vars{}
		if len(in.Vars) > 0 {
			if err := json.Unmarshal(in.Vars, &vars); err != nil {
				writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "vars must be a JSON object")
				return
			}
		}
		run, err := d.Runs.StartManual(r.Context(), actorFrom(r), chi.URLParam(r, "id"), vars)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, runIDDTO{RunID: run.ID})
	}
}
