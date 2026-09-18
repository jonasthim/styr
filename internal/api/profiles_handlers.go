package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
)

// registerProfilesRoutes mounts profile listing (any signed-in user) and
// management (admin only).
func registerProfilesRoutes(r chi.Router, d *Deps) {
	r.Get("/profiles", handleProfilesList(d))
	r.With(auth.RequireAdmin).Post("/profiles", handleProfilesCreate(d))
	r.With(auth.RequireAdmin).Patch("/profiles/{id}", handleProfilesPatch(d))
}

// allowedModes are the permission modes Styr accepts, mirroring
// internal/harness's validModes: the CLI's "skip all permission checks"
// mode (the skip-all-checks mode) is deliberately never accepted.
var allowedModes = map[string]bool{
	"default":     true,
	"acceptEdits": true,
	"plan":        true,
	"dontAsk":     true,
	"auto":        true,
}

type profileDTO struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Mode            string   `json:"mode"`
	AllowedTools    []string `json:"allowed_tools"`
	DisallowedTools []string `json:"disallowed_tools"`
	MaxTurns        int      `json:"max_turns"`
	Unattended      bool     `json:"unattended"`
	ApprovalTimeout int      `json:"approval_timeout"` // seconds
	Builtin         bool     `json:"builtin"`
	Model           string   `json:"model"`
	Effort          string   `json:"effort"`
}

func profileDTOFrom(p domain.Profile) profileDTO {
	return profileDTO{
		ID: p.ID, Name: p.Name, Mode: p.Mode, AllowedTools: p.AllowedTools, DisallowedTools: p.DisallowedTools,
		MaxTurns: p.MaxTurns, Unattended: p.Unattended, ApprovalTimeout: int(p.ApprovalTimeout / time.Second), Builtin: p.Builtin,
		Model: p.Model, Effort: p.Effort,
	}
}

// handleProfilesList is GET /api/v1/profiles.
func handleProfilesList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Profiles.List(r.Context())
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]profileDTO, 0, len(list))
		for _, p := range list {
			out = append(out, profileDTOFrom(p))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type profileInput struct {
	Name            *string   `json:"name"`
	Mode            *string   `json:"mode"`
	AllowedTools    *[]string `json:"allowed_tools"`
	DisallowedTools *[]string `json:"disallowed_tools"`
	MaxTurns        *int      `json:"max_turns"`
	Unattended      *bool     `json:"unattended"`
	ApprovalTimeout *int      `json:"approval_timeout"` // seconds
	Model           *string   `json:"model"`
	Effort          *string   `json:"effort"`
}

// handleProfilesCreate is POST /api/v1/profiles.
func handleProfilesCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in profileInput
		if err := decodeJSON(r, &in); err != nil || in.Name == nil || *in.Name == "" || in.Mode == nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "name and mode are required")
			return
		}
		if !allowedModes[*in.Mode] {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "mode is not allowed")
			return
		}
		if in.Effort != nil && !harness.ValidEffort(*in.Effort) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "effort is not allowed")
			return
		}
		p := domain.Profile{ID: uuid.NewString(), Name: *in.Name, Mode: *in.Mode, Builtin: false}
		if in.AllowedTools != nil {
			p.AllowedTools = *in.AllowedTools
		}
		if in.DisallowedTools != nil {
			p.DisallowedTools = *in.DisallowedTools
		}
		if in.MaxTurns != nil {
			p.MaxTurns = *in.MaxTurns
		}
		if in.Unattended != nil {
			p.Unattended = *in.Unattended
		}
		if in.ApprovalTimeout != nil {
			p.ApprovalTimeout = time.Duration(*in.ApprovalTimeout) * time.Second
		}
		if in.Model != nil {
			p.Model = *in.Model
		}
		if in.Effort != nil {
			p.Effort = *in.Effort
		}
		if err := d.Profiles.Create(r.Context(), p); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, profileDTOFrom(p))
	}
}

// handleProfilesPatch is PATCH /api/v1/profiles/{id}. Builtin rows only allow max_turns,
// approval_timeout, model and effort to change — the model and effort defaults are an
// operator preference, not part of what makes a builtin profile safe. Every profile (builtin
// or not) rejects a mode that is not in allowedModes and an effort that is not a valid level.
func handleProfilesPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		p, err := d.Profiles.Get(r.Context(), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		var in profileInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if in.Mode != nil && !allowedModes[*in.Mode] {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "mode is not allowed")
			return
		}
		if in.Effort != nil && !harness.ValidEffort(*in.Effort) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "effort is not allowed")
			return
		}
		if p.Builtin && (in.Name != nil || in.Mode != nil || in.AllowedTools != nil || in.DisallowedTools != nil || in.Unattended != nil) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "builtin profiles only allow max_turns, approval_timeout, model and effort to change")
			return
		}
		if in.Name != nil {
			p.Name = *in.Name
		}
		if in.Mode != nil {
			p.Mode = *in.Mode
		}
		if in.AllowedTools != nil {
			p.AllowedTools = *in.AllowedTools
		}
		if in.DisallowedTools != nil {
			p.DisallowedTools = *in.DisallowedTools
		}
		if in.MaxTurns != nil {
			p.MaxTurns = *in.MaxTurns
		}
		if in.Unattended != nil {
			p.Unattended = *in.Unattended
		}
		if in.ApprovalTimeout != nil {
			p.ApprovalTimeout = time.Duration(*in.ApprovalTimeout) * time.Second
		}
		if in.Model != nil {
			p.Model = *in.Model
		}
		if in.Effort != nil {
			p.Effort = *in.Effort
		}
		if err := d.Profiles.Update(r.Context(), *p); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, profileDTOFrom(*p))
	}
}
