package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/runs"
)

// registerLoopsRoutes mounts the read-mostly loops routes: a loop is
// created implicitly by starting a run from a looping template (see
// handleTemplatesRun and the trigger/schedule/webhook paths that call
// Engine.Start), never by a direct POST here.
func registerLoopsRoutes(r chi.Router, d *Deps) {
	r.Get("/loops", handleLoopsList(d))
	r.Get("/loops/{id}", handleLoopsGet(d))
	r.Post("/loops/{id}/stop", handleLoopsStop(d))
}

// loopDTO is domain.Loop shaped for JSON.
type loopDTO struct {
	ID            string    `json:"id"`
	TemplateID    string    `json:"template_id"`
	SessionID     *string   `json:"session_id"`
	Origin        string    `json:"origin"`
	OriginRef     string    `json:"origin_ref"`
	UntilField    string    `json:"until_field"`
	MaxIterations int       `json:"max_iterations"`
	Iteration     int       `json:"iteration"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func loopDTOFrom(l domain.Loop) loopDTO {
	return loopDTO{
		ID: l.ID, TemplateID: l.TemplateID, SessionID: l.SessionID, Origin: l.Origin, OriginRef: l.OriginRef,
		UntilField: l.UntilField, MaxIterations: l.MaxIterations, Iteration: l.Iteration, State: string(l.State),
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

// loopViewDTO is runs.LoopView shaped for JSON: the loop plus every
// iteration's run (in order) and the template they all render, nil when
// the template was since deleted.
type loopViewDTO struct {
	Loop     loopDTO      `json:"loop"`
	Runs     []runDTO     `json:"runs"`
	Template *templateDTO `json:"template"`
}

func loopViewDTOFrom(v runs.LoopView) loopViewDTO {
	out := loopViewDTO{Loop: loopDTOFrom(v.Loop), Runs: make([]runDTO, 0, len(v.Runs))}
	for _, run := range v.Runs {
		out.Runs = append(out.Runs, runDTOFrom(run))
	}
	if v.Template != nil {
		t := templateDTOFrom(*v.Template)
		out.Template = &t
	}
	return out
}

// handleLoopsList is GET /api/v1/loops?state=&limit=.
func handleLoopsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit := 0
		if s := q.Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}
		list, err := d.Runs.ListLoops(r.Context(), q.Get("state"), limit)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]loopDTO, 0, len(list))
		for _, l := range list {
			out = append(out, loopDTOFrom(l))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleLoopsGet is GET /api/v1/loops/{id}.
func handleLoopsGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := d.Runs.GetLoop(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, loopViewDTOFrom(v))
	}
}

// handleLoopsStop is POST /api/v1/loops/{id}/stop: ends a running loop and
// closes its session. Stopping a loop that is not running is a 409
// (domain.ErrConflict, mapped by WriteError).
func handleLoopsStop(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Runs.Stop(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
