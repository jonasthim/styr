package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
)

// registerRunsRoutes mounts the read-only runs routes. Runs are visible to
// every signed-in user (the v0.1 decision for unattended work), so there is
// no per-run visibility check here beyond RequireUser.
func registerRunsRoutes(r chi.Router, d *Deps) {
	r.Get("/runs", handleRunsList(d))
	r.Get("/runs/{id}", handleRunsGet(d))
}

type runDTO struct {
	ID         string  `json:"id"`
	SessionID  string  `json:"session_id"`
	TemplateID *string `json:"template_id"`
	TriggerID  *string `json:"trigger_id"`
	DeliveryID *string `json:"delivery_id"`
	Origin     string  `json:"origin"`
	// StepRunID is set when this run is one attempt of a pipeline step
	// (origin "pipeline"), nil for every other run.
	StepRunID  *string         `json:"step_run_id"`
	LoopID     string          `json:"loop_id"`
	Iteration  int             `json:"iteration"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Outcome    string          `json:"outcome"`
	Report     json.RawMessage `json:"report"`
	Summary    string          `json:"summary"`
	CostUSD    float64         `json:"cost_usd"`
}

func runDTOFrom(run domain.Run) runDTO {
	return runDTO{
		ID: run.ID, SessionID: run.SessionID, TemplateID: run.TemplateID, TriggerID: run.TriggerID,
		DeliveryID: run.DeliveryID, Origin: run.Origin, StepRunID: run.StepRunID,
		LoopID: run.LoopID, Iteration: run.Iteration,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		Outcome: string(run.Outcome), Report: run.Report, Summary: run.Summary, CostUSD: run.CostUSD,
	}
}

// runViewDTO is domain.RunView shaped for JSON: the run itself plus the
// session, delivery, template and loop it was started from, each nil when
// that record is unavailable (e.g. the template was since deleted, or the
// run belongs to no loop).
type runViewDTO struct {
	Run      runDTO       `json:"run"`
	Session  *sessionDTO  `json:"session"`
	Delivery *deliveryDTO `json:"delivery"`
	Template *templateDTO `json:"template"`
	Loop     *loopDTO     `json:"loop"`
	// Step is the pipeline step run this run is one attempt of and
	// Pipeline the pipeline it belongs to, both nil outside a pipeline.
	Step     *stepRunDTO  `json:"step"`
	Pipeline *pipelineDTO `json:"pipeline"`
}

func runViewDTOFrom(v domain.RunView) runViewDTO {
	out := runViewDTO{Run: runDTOFrom(v.Run)}
	if v.Session != nil {
		s := sessionDTOFrom(*v.Session)
		out.Session = &s
	}
	if v.Delivery != nil {
		dl := deliveryDTOFrom(*v.Delivery)
		out.Delivery = &dl
	}
	if v.Template != nil {
		t := templateDTOFrom(*v.Template)
		out.Template = &t
	}
	if v.Loop != nil {
		l := loopDTOFrom(*v.Loop)
		out.Loop = &l
	}
	if v.Step != nil {
		sr := stepRunDTOFrom(*v.Step)
		out.Step = &sr
	}
	if v.Pipeline != nil {
		pl := pipelineDTOFrom(*v.Pipeline)
		out.Pipeline = &pl
	}
	return out
}

// handleRunsList is GET /api/v1/runs?outcome=&trigger=&loop_id=&limit=.
func handleRunsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := domain.RunFilter{Outcome: q.Get("outcome"), TriggerID: q.Get("trigger"), LoopID: q.Get("loop_id")}
		if s := q.Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				filter.Limit = n
			}
		}
		list, err := d.Runs.List(r.Context(), filter)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]runViewDTO, 0, len(list))
		for _, v := range list {
			out = append(out, runViewDTOFrom(v))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleRunsGet is GET /api/v1/runs/{id}.
func handleRunsGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := d.Runs.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, runViewDTOFrom(v))
	}
}
