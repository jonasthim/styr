package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/schedules"
)

// defaultScheduleFiringsLimit is used for GET /schedules/{id}/firings when
// the caller omits ?limit= (the service itself applies its own default and
// cap, this is just "no limit requested" for the query parse).
const defaultScheduleFiringsLimit = 0

// registerSchedulesRoutes mounts schedule CRUD, run-now, firings and the
// cron preview routes. POST /schedules/preview is registered as a literal
// path ahead of {id}'s wildcard, same as GET /triggers/samples/{kind};
// chi's radix tree matches the literal segment regardless of registration
// order. Visibility and the "Shared requires admin" rule are enforced by
// SchedulesService.
func registerSchedulesRoutes(r chi.Router, d *Deps) {
	r.Get("/schedules", handleSchedulesList(d))
	r.Post("/schedules", handleSchedulesCreate(d))
	r.Post("/schedules/preview", handleSchedulesPreview(d))
	r.Get("/schedules/{id}", handleSchedulesGet(d))
	r.Patch("/schedules/{id}", handleSchedulesPatch(d))
	r.Delete("/schedules/{id}", handleSchedulesDelete(d))
	r.Post("/schedules/{id}/run", handleSchedulesRun(d))
	r.Get("/schedules/{id}/firings", handleSchedulesFirings(d))
}

// scheduleDTO is domain.Schedule shaped for JSON: exactly the fields the
// v0.4 API contract documents, nothing more.
type scheduleDTO struct {
	ID         string  `json:"id"`
	OwnerID    *string `json:"owner_id"`
	Name       string  `json:"name"`
	TemplateID string  `json:"template_id"`
	// PipelineID is the alternative to TemplateID; exactly one is set.
	PipelineID  *string         `json:"pipeline_id"`
	Cron        string          `json:"cron"`
	Enabled     bool            `json:"enabled"`
	Vars        json.RawMessage `json:"vars"`
	LastRunAt   *time.Time      `json:"last_run_at"`
	LastOutcome string          `json:"last_outcome"`
	NextRunAt   *time.Time      `json:"next_run_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func scheduleDTOFrom(sc domain.Schedule) scheduleDTO {
	return scheduleDTO{
		ID: sc.ID, OwnerID: sc.OwnerID, Name: sc.Name, TemplateID: sc.TemplateID, PipelineID: sc.PipelineID, Cron: sc.Cron,
		Enabled: sc.Enabled, Vars: sc.Vars, LastRunAt: sc.LastRunAt, LastOutcome: sc.LastOutcome,
		NextRunAt: sc.NextRunAt, CreatedAt: sc.CreatedAt, UpdatedAt: sc.UpdatedAt,
	}
}

type scheduleInput struct {
	Name       string          `json:"name"`
	TemplateID string          `json:"template_id"`
	PipelineID *string         `json:"pipeline_id"`
	Cron       string          `json:"cron"`
	Vars       json.RawMessage `json:"vars"`
	Enabled    *bool           `json:"enabled"`
	Shared     bool            `json:"shared"`
}

func (in scheduleInput) toDomain() domain.ScheduleInput {
	return domain.ScheduleInput{
		Name: in.Name, TemplateID: in.TemplateID, PipelineID: derefString(in.PipelineID), Cron: in.Cron, Vars: in.Vars,
		Enabled: in.Enabled, Shared: in.Shared,
	}
}

// handleSchedulesList is GET /api/v1/schedules.
func handleSchedulesList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Schedules.List(r.Context(), actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]scheduleDTO, 0, len(list))
		for _, sc := range list {
			out = append(out, scheduleDTOFrom(sc))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleSchedulesCreate is POST /api/v1/schedules.
func handleSchedulesCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in scheduleInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if !exactlyOneOfTemplateOrPipeline(in.TemplateID, in.PipelineID) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "exactly one of template_id or pipeline_id is required")
			return
		}
		sc, err := d.Schedules.Create(r.Context(), actorFrom(r), in.toDomain())
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, scheduleDTOFrom(sc))
	}
}

// handleSchedulesGet is GET /api/v1/schedules/{id}.
func handleSchedulesGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, err := d.Schedules.Get(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, scheduleDTOFrom(sc))
	}
}

// handleSchedulesPatch is PATCH /api/v1/schedules/{id}: a full replace of
// the schedule's mutable configuration, same rule as PATCH /triggers/{id}.
func handleSchedulesPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in scheduleInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if !exactlyOneOfTemplateOrPipeline(in.TemplateID, in.PipelineID) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "exactly one of template_id or pipeline_id is required")
			return
		}
		sc, err := d.Schedules.Update(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.toDomain())
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, scheduleDTOFrom(sc))
	}
}

// handleSchedulesDelete is DELETE /api/v1/schedules/{id}.
func handleSchedulesDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Schedules.Delete(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// scheduleRunStartedDTO is POST /api/v1/schedules/{id}/run's 202 body.
// RunID is a run id for a schedule that starts a template, and the
// "pr:<pipeline run id>" reference for one that starts a pipeline (the
// same form its firings record). PipelineRunID repeats that id unprefixed
// so the UI can link straight to the pipeline run page — a pipeline run is
// not a run row, so /runs/{that id} would 404.
type scheduleRunStartedDTO struct {
	RunID         string `json:"run_id"`
	PipelineRunID string `json:"pipeline_run_id,omitempty"`
}

// handleSchedulesRun is POST /api/v1/schedules/{id}/run: fires the
// schedule immediately, ignoring its cron and next_run_at.
func handleSchedulesRun(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run, err := d.Schedules.RunNow(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := scheduleRunStartedDTO{RunID: run.ID}
		if id, isPipeline := strings.CutPrefix(run.ID, schedules.PipelineRunRefPrefix); isPipeline {
			out.PipelineRunID = id
		}
		WriteJSON(w, http.StatusAccepted, out)
	}
}

type scheduleFiringDTO struct {
	ID         string    `json:"id"`
	ScheduleID string    `json:"schedule_id"`
	FiredAt    time.Time `json:"fired_at"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	RunID      *string   `json:"run_id"`
}

func scheduleFiringDTOFrom(f domain.ScheduleFiring) scheduleFiringDTO {
	return scheduleFiringDTO{
		ID: f.ID, ScheduleID: f.ScheduleID, FiredAt: f.FiredAt, Status: string(f.Status),
		Reason: f.Reason, RunID: f.RunID,
	}
}

// handleSchedulesFirings is GET /api/v1/schedules/{id}/firings?limit=.
func handleSchedulesFirings(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := defaultScheduleFiringsLimit
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}
		list, err := d.Schedules.Firings(r.Context(), actorFrom(r), chi.URLParam(r, "id"), limit)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]scheduleFiringDTO, 0, len(list))
		for _, f := range list {
			out = append(out, scheduleFiringDTOFrom(f))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type schedulePreviewInput struct {
	Cron string `json:"cron"`
}

type schedulePreviewDTO struct {
	Next        []string `json:"next"`
	Description string   `json:"description"`
}

// handleSchedulesPreview is POST /api/v1/schedules/preview: {cron} -> the
// next 5 fire times plus a human-readable description, with no schedule
// created. A cron expression that fails to parse (including an empty one)
// is a 422 with code "invalid_cron", distinct from the generic "invalid"
// code every other validation failure uses, so the UI can point the error
// at the cron field specifically.
func handleSchedulesPreview(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in schedulePreviewInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_cron", "invalid request body")
			return
		}
		preview, err := d.Schedules.Preview(in.Cron)
		if err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_cron", userMessage(err, domain.ErrInvalid))
			return
		}
		next := make([]string, len(preview.Next))
		for i, t := range preview.Next {
			next[i] = t.UTC().Format(time.RFC3339)
		}
		WriteJSON(w, http.StatusOK, schedulePreviewDTO{Next: next, Description: preview.Description})
	}
}
