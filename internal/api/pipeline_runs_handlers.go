package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/pipelines"
)

// registerPipelineRunsRoutes mounts the pipeline-run routes: list, get,
// cancel and retry-failed. Visibility is enforced by PipelinesService, the
// same as every other resource's handlers.
func registerPipelineRunsRoutes(r chi.Router, d *Deps) {
	r.Get("/pipeline-runs", handlePipelineRunsList(d))
	r.Get("/pipeline-runs/{id}", handlePipelineRunsGet(d))
	r.Post("/pipeline-runs/{id}/cancel", handlePipelineRunsCancel(d))
	r.Post("/pipeline-runs/{id}/retry-failed", handlePipelineRunsRetryFailed(d))
}

// pipelineRunDTO is domain.PipelineRun shaped for JSON.
type pipelineRunDTO struct {
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

func pipelineRunDTOFrom(pr domain.PipelineRun) pipelineRunDTO {
	return pipelineRunDTO{
		ID: pr.ID, PipelineID: pr.PipelineID, Origin: pr.Origin, OriginRef: pr.OriginRef,
		Input: normalizeRawJSON(pr.Input), State: string(pr.State),
		StartedAt: pr.StartedAt, FinishedAt: pr.FinishedAt, CostUSD: pr.CostUSD,
	}
}

// stepRunDTO is domain.StepRun shaped for JSON: id, pipeline_run_id,
// step_id, index_in_fanout, item, run_id, attempt, state, report (object
// or null), started_at, finished_at, worktree.
type stepRunDTO struct {
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
}

func stepRunDTOFrom(s domain.StepRun) stepRunDTO {
	return stepRunDTO{
		ID: s.ID, PipelineRunID: s.PipelineRunID, StepID: s.StepID, IndexInFanout: s.IndexInFanout,
		Item: s.Item, RunID: s.RunID, Attempt: s.Attempt, State: string(s.State),
		Report: normalizeRawJSON(s.Report), StartedAt: s.StartedAt, FinishedAt: s.FinishedAt, Worktree: s.Worktree,
	}
}

// normalizeRawJSON returns nil (marshals to JSON null) for an empty
// json.RawMessage — the repository layer may store "" for "no value yet"
// rather than leaving the field nil, which json.RawMessage's own
// MarshalJSON would otherwise pass through as invalid empty output.
func normalizeRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// stepViewDTO is one entry of GET /pipeline-runs/{id}'s steps array: a
// StepRun's fields (promoted from the embedded stepRunDTO) plus the Run
// summary of its current attempt, null until the executor has started one.
type stepViewDTO struct {
	stepRunDTO
	Run *runDTO `json:"run"`
}

func stepViewDTOFrom(sv pipelines.StepView) stepViewDTO {
	out := stepViewDTO{stepRunDTO: stepRunDTOFrom(sv.Step)}
	if sv.RunSummary != nil {
		rd := runDTOFrom(*sv.RunSummary)
		out.Run = &rd
	}
	return out
}

// pipelineRunViewDTO is pipelines.RunView shaped for JSON: the run, the
// pipeline it belongs to, every step (with its current attempt's Run
// summary) and the pipeline's graph.
type pipelineRunViewDTO struct {
	Run      pipelineRunDTO `json:"run"`
	Pipeline pipelineDTO    `json:"pipeline"`
	Steps    []stepViewDTO  `json:"steps"`
	Graph    graphDTO       `json:"graph"`
}

func pipelineRunViewDTOFrom(v pipelines.RunView) pipelineRunViewDTO {
	steps := make([]stepViewDTO, len(v.Steps))
	for i, sv := range v.Steps {
		steps[i] = stepViewDTOFrom(sv)
	}
	return pipelineRunViewDTO{
		Run: pipelineRunDTOFrom(v.Run), Pipeline: pipelineDTOFrom(v.Pipeline),
		Steps: steps, Graph: graphDTOFrom(v.Graph),
	}
}

// handlePipelineRunsList is GET /api/v1/pipeline-runs?pipeline=&state=&limit=.
func handlePipelineRunsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := domain.PipelineRunFilter{PipelineID: q.Get("pipeline"), State: q.Get("state")}
		if s := q.Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				filter.Limit = n
			}
		}
		list, err := d.Pipelines.ListRuns(r.Context(), actorFrom(r), filter)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]pipelineRunDTO, 0, len(list))
		for _, pr := range list {
			out = append(out, pipelineRunDTOFrom(pr))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handlePipelineRunsGet is GET /api/v1/pipeline-runs/{id}.
func handlePipelineRunsGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := d.Pipelines.GetRun(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, pipelineRunViewDTOFrom(v))
	}
}

// handlePipelineRunsCancel is POST /api/v1/pipeline-runs/{id}/cancel:
// cancels every running step-run and closes their sessions. Cancelling a
// pipeline run that is not running is a 409 (domain.ErrConflict, mapped by
// WriteError).
func handlePipelineRunsCancel(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Pipelines.Cancel(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// handlePipelineRunsRetryFailed is POST
// /api/v1/pipeline-runs/{id}/retry-failed: re-runs every failed step-run
// (a new attempt) and continues the pipeline. Retrying a pipeline run with
// no failed steps is a 409 (domain.ErrConflict, mapped by WriteError).
func handlePipelineRunsRetryFailed(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Pipelines.RetryFailed(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
