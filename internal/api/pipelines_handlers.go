package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/pipelines"
	"github.com/jonasthim/styr/internal/templates"
)

// registerPipelinesRoutes mounts pipeline CRUD, validation and start
// routes. POST /pipelines/validate is registered as a literal path ahead
// of {id}'s wildcard, same as GET /triggers/samples/{kind}; chi's radix
// tree matches the literal segment regardless of registration order.
// Visibility and the "Shared requires admin" rule are enforced by
// PipelinesService.
func registerPipelinesRoutes(r chi.Router, d *Deps) {
	r.Get("/pipelines", handlePipelinesList(d))
	r.Post("/pipelines", handlePipelinesCreate(d))
	r.Post("/pipelines/validate", handlePipelinesValidate(d))
	r.Get("/pipelines/{id}", handlePipelinesGet(d))
	r.Patch("/pipelines/{id}", handlePipelinesPatch(d))
	r.Delete("/pipelines/{id}", handlePipelinesDelete(d))
	r.Post("/pipelines/{id}/start", handlePipelinesStart(d))
}

// pipelineDTO is domain.Pipeline shaped for JSON: exactly the fields the
// v0.5 API contract documents.
type pipelineDTO struct {
	ID          string    `json:"id"`
	OwnerID     *string   `json:"owner_id"`
	Name        string    `json:"name"`
	WorkspaceID string    `json:"workspace_id"`
	YAML        string    `json:"yaml"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func pipelineDTOFrom(p domain.Pipeline) pipelineDTO {
	return pipelineDTO{
		ID: p.ID, OwnerID: p.OwnerID, Name: p.Name, WorkspaceID: p.WorkspaceID, YAML: p.YAML,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

type pipelineInput struct {
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id"`
	YAML        string `json:"yaml"`
	Shared      bool   `json:"shared"`
}

func (in pipelineInput) toDomain() domain.PipelineInput {
	return domain.PipelineInput{Name: in.Name, WorkspaceID: in.WorkspaceID, YAML: in.YAML, Shared: in.Shared}
}

// problemDTO is pipelines.Problem shaped for JSON.
type problemDTO struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

func problemDTOsFrom(problems []pipelines.Problem) []problemDTO {
	out := make([]problemDTO, len(problems))
	for i, p := range problems {
		out[i] = problemDTO{Line: p.Line, Message: p.Message}
	}
	return out
}

// nodeDTO and edgeDTO are pipelines.Node/Edge shaped for JSON.
type nodeDTO struct {
	ID       string `json:"id"`
	Template string `json:"template"`
	Worktree string `json:"worktree"`
	Foreach  bool   `json:"foreach"`
}

type edgeDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type graphDTO struct {
	Nodes []nodeDTO `json:"nodes"`
	Edges []edgeDTO `json:"edges"`
}

func graphDTOFrom(g pipelines.Graph) graphDTO {
	nodes := make([]nodeDTO, len(g.Nodes))
	for i, n := range g.Nodes {
		nodes[i] = nodeDTO{ID: n.ID, Template: n.Template, Worktree: n.Worktree, Foreach: n.Foreach}
	}
	edges := make([]edgeDTO, len(g.Edges))
	for i, e := range g.Edges {
		edges[i] = edgeDTO{From: e.From, To: e.To}
	}
	return graphDTO{Nodes: nodes, Edges: edges}
}

// pipelineErrorEnvelope is the 422 body for POST/PATCH /pipelines when the
// yaml fails validation: the ordinary {error:{code,message}} envelope plus
// the full problem list, so the UI can point at every error (with its
// line) rather than just the first.
type pipelineErrorEnvelope struct {
	Error  errorBody    `json:"error"`
	Errors []problemDTO `json:"errors"`
}

// writePipelineError maps an error from CreatePipeline/UpdatePipeline: a
// *pipelines.ValidationError (wrapping domain.ErrInvalid with the full
// problem list) becomes 422 "invalid_pipeline" with the problems; anything
// else falls back to WriteError's ordinary domain-sentinel mapping.
func writePipelineError(w http.ResponseWriter, err error) {
	var verr *pipelines.ValidationError
	if errors.As(err, &verr) {
		msg := "pipeline definition is invalid"
		if len(verr.Problems) > 0 {
			msg = verr.Problems[0].Message
		}
		WriteJSON(w, http.StatusUnprocessableEntity, pipelineErrorEnvelope{
			Error:  errorBody{Code: "invalid_pipeline", Message: msg},
			Errors: problemDTOsFrom(verr.Problems),
		})
		return
	}
	WriteError(w, err)
}

// handlePipelinesList is GET /api/v1/pipelines.
func handlePipelinesList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Pipelines.ListPipelines(r.Context(), actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]pipelineDTO, 0, len(list))
		for _, p := range list {
			out = append(out, pipelineDTOFrom(p))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handlePipelinesCreate is POST /api/v1/pipelines: 201 Pipeline, or 422
// invalid_pipeline with the full problem list when the yaml fails
// validation.
func handlePipelinesCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in pipelineInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		p, err := d.Pipelines.CreatePipeline(r.Context(), actorFrom(r), in.toDomain())
		if err != nil {
			writePipelineError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, pipelineDTOFrom(p))
	}
}

// pipelineValidateInput is POST /api/v1/pipelines/validate's request body.
type pipelineValidateInput struct {
	YAML        string `json:"yaml"`
	WorkspaceID string `json:"workspace_id"`
}

// validationResultDTO is pipelines.ValidationResult shaped for JSON.
type validationResultDTO struct {
	OK     bool         `json:"ok"`
	Errors []problemDTO `json:"errors"`
	Graph  graphDTO     `json:"graph"`
}

// handlePipelinesValidate is POST /api/v1/pipelines/validate: always 200,
// even when the definition is invalid — ok is false and errors lists every
// problem in that case, so the UI's live validation preview never has to
// special-case a non-2xx status for "your yaml has a mistake".
func handlePipelinesValidate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in pipelineValidateInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		res, err := d.Pipelines.Validate(r.Context(), actorFrom(r), in.WorkspaceID, []byte(in.YAML))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, validationResultDTO{
			OK: res.OK, Errors: problemDTOsFrom(res.Problems), Graph: graphDTOFrom(res.Graph),
		})
	}
}

// handlePipelinesGet is GET /api/v1/pipelines/{id}.
func handlePipelinesGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := d.Pipelines.GetPipeline(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, pipelineDTOFrom(p))
	}
}

// handlePipelinesPatch is PATCH /api/v1/pipelines/{id}: a full replace of
// the pipeline's name, workspace and yaml, re-validated the same way as
// create.
func handlePipelinesPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in pipelineInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		p, err := d.Pipelines.UpdatePipeline(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.toDomain())
		if err != nil {
			writePipelineError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, pipelineDTOFrom(p))
	}
}

// handlePipelinesDelete is DELETE /api/v1/pipelines/{id}.
func handlePipelinesDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Pipelines.DeletePipeline(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// pipelineStartInput is POST /api/v1/pipelines/{id}/start's request body;
// input is optional, matching POST /templates/{id}/run's {vars?}.
type pipelineStartInput struct {
	Input json.RawMessage `json:"input"`
}

// pipelineRunIDDTO is the {pipeline_run_id} shape POST
// /pipelines/{id}/start answers with.
type pipelineRunIDDTO struct {
	PipelineRunID string `json:"pipeline_run_id"`
}

// handlePipelinesStart is POST /api/v1/pipelines/{id}/start: starts a
// pipeline run by hand — origin ui, no origin_ref, same shape as
// POST /templates/{id}/run.
func handlePipelinesStart(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in pipelineStartInput
		if err := decodeOptionalJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		vars := templates.Vars{}
		if len(in.Input) > 0 {
			if err := json.Unmarshal(in.Input, &vars); err != nil {
				writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "input must be a JSON object")
				return
			}
		}
		run, err := d.Pipelines.Start(r.Context(), actorFrom(r), chi.URLParam(r, "id"), vars, domain.OriginUI, "")
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, pipelineRunIDDTO{PipelineRunID: run.ID})
	}
}
