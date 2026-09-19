package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

// defaultDeliveriesLimit is used for GET /triggers/{id}/deliveries when the
// caller omits ?limit=.
const defaultDeliveriesLimit = 50

// allowedSampleKinds are the trigger kinds GET /triggers/samples/{kind}
// serves a sample for; any other kind is a 404, unlike
// templates.SamplePayload itself, which falls back to the generic sample
// for callers (Normalize, dedupe rendering) that always want *something*.
var allowedSampleKinds = map[string]bool{"generic": true, "grafana": true, "github": true}

// registerTriggersRoutes mounts trigger CRUD, secret rotation, deliveries
// and test routes, plus the per-kind sample payload route. Visibility and
// the "Shared requires admin" rule are enforced by TriggersService.
func registerTriggersRoutes(r chi.Router, d *Deps) {
	r.Get("/triggers", handleTriggersList(d))
	r.Post("/triggers", handleTriggersCreate(d))
	r.Get("/triggers/samples/{kind}", handleTriggersSample())
	r.Get("/triggers/{id}", handleTriggersGet(d))
	r.Patch("/triggers/{id}", handleTriggersPatch(d))
	r.Delete("/triggers/{id}", handleTriggersDelete(d))
	r.Post("/triggers/{id}/rotate-secret", handleTriggersRotateSecret(d))
	r.Get("/triggers/{id}/deliveries", handleTriggersDeliveries(d))
	r.Post("/triggers/{id}/test", handleTriggersTest(d))
}

// triggerDTO never carries the secret hash (domain.Trigger tags SecretHash
// json:"-", but that field is also simply not copied here for clarity) or
// the plaintext secret — that appears only in triggerCreateDTO (create) and
// triggerSecretDTO (rotate).
type triggerDTO struct {
	ID         string  `json:"id"`
	OwnerID    *string `json:"owner_id"`
	Name       string  `json:"name"`
	Slug       string  `json:"slug"`
	Kind       string  `json:"kind"`
	SecretHint string  `json:"secret_hint"`
	TemplateID string  `json:"template_id"`
	// PipelineID is the alternative to TemplateID; exactly one is set.
	PipelineID        *string    `json:"pipeline_id"`
	Enabled           bool       `json:"enabled"`
	DedupeKeyTemplate string     `json:"dedupe_key_template"`
	CooldownS         int        `json:"cooldown_s"`
	StormCapPerHour   int        `json:"storm_cap_per_hour"`
	RunOnResolved     bool       `json:"run_on_resolved"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastDeliveryAt    *time.Time `json:"last_delivery_at"`
}

func triggerDTOFrom(t domain.Trigger) triggerDTO {
	return triggerDTO{
		ID: t.ID, OwnerID: t.OwnerID, Name: t.Name, Slug: t.Slug, Kind: string(t.Kind),
		SecretHint: t.SecretHint, TemplateID: t.TemplateID, PipelineID: t.PipelineID, Enabled: t.Enabled,
		DedupeKeyTemplate: t.DedupeKeyTemplate, CooldownS: t.CooldownS, StormCapPerHour: t.StormCapPerHour,
		RunOnResolved: t.RunOnResolved, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, LastDeliveryAt: t.LastDeliveryAt,
	}
}

type triggerCreateDTO struct {
	Trigger triggerDTO `json:"trigger"`
	Secret  string     `json:"secret"`
}

type triggerSecretDTO struct {
	Secret string `json:"secret"`
}

type triggerInput struct {
	Name              string  `json:"name"`
	Kind              string  `json:"kind"`
	TemplateID        string  `json:"template_id"`
	PipelineID        *string `json:"pipeline_id"`
	DedupeKeyTemplate string  `json:"dedupe_key_template"`
	CooldownS         int     `json:"cooldown_s"`
	StormCapPerHour   int     `json:"storm_cap_per_hour"`
	RunOnResolved     bool    `json:"run_on_resolved"`
	Shared            bool    `json:"shared"`
	Enabled           *bool   `json:"enabled"`
}

func (in triggerInput) toDomain() domain.TriggerInput {
	return domain.TriggerInput{
		Name: in.Name, Kind: in.Kind, TemplateID: in.TemplateID, PipelineID: derefString(in.PipelineID),
		DedupeKeyTemplate: in.DedupeKeyTemplate,
		CooldownS:         in.CooldownS, StormCapPerHour: in.StormCapPerHour, RunOnResolved: in.RunOnResolved,
		Shared: in.Shared, Enabled: in.Enabled,
	}
}

// exactlyOneOfTemplateOrPipeline reports whether exactly one of templateID
// (non-empty) and pipelineID (non-nil and non-empty) is set — the rule
// every trigger and schedule create/update must satisfy. Checked here so
// the 422 comes back before ever reaching TriggersService/SchedulesService,
// which also enforce it, defensively, against a caller that bypasses the
// API (e.g. a future gRPC surface).
func exactlyOneOfTemplateOrPipeline(templateID string, pipelineID *string) bool {
	hasTemplate := templateID != ""
	hasPipeline := pipelineID != nil && *pipelineID != ""
	return hasTemplate != hasPipeline
}

// handleTriggersList is GET /api/v1/triggers.
func handleTriggersList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Triggers.ListTriggers(r.Context(), actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]triggerDTO, 0, len(list))
		for _, t := range list {
			out = append(out, triggerDTOFrom(t))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleTriggersCreate is POST /api/v1/triggers: 201 {trigger, secret} —
// the plaintext secret is returned only this once.
func handleTriggersCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in triggerInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if !exactlyOneOfTemplateOrPipeline(in.TemplateID, in.PipelineID) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "exactly one of template_id or pipeline_id is required")
			return
		}
		t, secret, err := d.Triggers.CreateTrigger(r.Context(), actorFrom(r), in.toDomain())
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, triggerCreateDTO{Trigger: triggerDTOFrom(t), Secret: secret})
	}
}

// handleTriggersSample is GET /api/v1/triggers/samples/{kind}: a realistic
// example webhook body for kind, for "send test payload" and template
// dry-run preview in the UI. An unrecognized kind is a 404.
func handleTriggersSample() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := chi.URLParam(r, "kind")
		if !allowedSampleKinds[kind] {
			writeErrorCode(w, http.StatusNotFound, "not_found", "unknown sample kind")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(templates.SamplePayload(kind))
	}
}

// handleTriggersGet is GET /api/v1/triggers/{id}.
func handleTriggersGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, err := d.Triggers.GetTrigger(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, triggerDTOFrom(t))
	}
}

// handleTriggersPatch is PATCH /api/v1/triggers/{id}.
func handleTriggersPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in triggerInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if !exactlyOneOfTemplateOrPipeline(in.TemplateID, in.PipelineID) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "exactly one of template_id or pipeline_id is required")
			return
		}
		t, err := d.Triggers.UpdateTrigger(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.toDomain())
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, triggerDTOFrom(t))
	}
}

// handleTriggersDelete is DELETE /api/v1/triggers/{id}.
func handleTriggersDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Triggers.DeleteTrigger(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleTriggersRotateSecret is POST /api/v1/triggers/{id}/rotate-secret:
// {secret} — the new plaintext secret, shown only this once.
func handleTriggersRotateSecret(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret, err := d.Triggers.RotateSecret(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, triggerSecretDTO{Secret: secret})
	}
}

// handleTriggersDeliveries is GET /api/v1/triggers/{id}/deliveries?limit=50.
func handleTriggersDeliveries(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := defaultDeliveriesLimit
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}
		list, err := d.Triggers.ListDeliveries(r.Context(), actorFrom(r), chi.URLParam(r, "id"), limit)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]deliveryDTO, 0, len(list))
		for _, dl := range list {
			out = append(out, deliveryDTOFrom(dl))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type triggerTestInput struct {
	Payload json.RawMessage `json:"payload"`
	Force   bool            `json:"force"`
}

// handleTriggersTest is POST /api/v1/triggers/{id}/test: runs the full
// delivery pipeline synchronously against payload, as if it had arrived at
// POST /hooks/{slug}, respecting dedupe/cooldown/storm unless force is
// true. The response is the same {delivery_id, status, run_id?} shape as
// the inbound hook endpoint's.
func handleTriggersTest(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in triggerTestInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		dl, err := d.Triggers.Test(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.Payload, in.Force)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, deliveryResultDTOFrom(dl))
	}
}
