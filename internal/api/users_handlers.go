package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/domain"
)

// registerUsersRoutes mounts user administration and server-wide settings
// (both admin-only).
func registerUsersRoutes(r chi.Router, d *Deps) {
	r.With(auth.RequireAdmin).Get("/users", handleUsersList(d))
	r.With(auth.RequireAdmin).Patch("/users/{id}", handleUsersPatch(d))

	r.With(auth.RequireAdmin).Get("/settings", handleSettingsGet(d))
	r.With(auth.RequireAdmin).Put("/settings/service-token", handleSettingsServiceTokenPut(d))
}

type userDTO struct {
	ID          string          `json:"id"`
	Email       string          `json:"email"`
	DisplayName string          `json:"display_name"`
	AvatarURL   string          `json:"avatar_url"`
	Role        string          `json:"role"`
	Prefs       json.RawMessage `json:"prefs"`
}

func userDTOFrom(u domain.User) userDTO {
	return userDTO{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, AvatarURL: u.AvatarURL, Role: string(u.Role), Prefs: u.Prefs}
}

// handleUsersList is GET /api/v1/users.
func handleUsersList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := d.Users.List(r.Context())
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]userDTO, 0, len(users))
		for _, u := range users {
			out = append(out, userDTOFrom(u))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type userPatchInput struct {
	Role string `json:"role"`
}

// handleUsersPatch is PATCH /api/v1/users/{id}: changes a user's role.
func handleUsersPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var in userPatchInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		role := domain.Role(in.Role)
		if role != domain.RoleAdmin && role != domain.RoleMember {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "role must be admin or member")
			return
		}
		if err := d.Users.UpdateRole(r.Context(), id, role); err != nil {
			WriteError(w, err)
			return
		}
		u, err := d.Users.GetByID(r.Context(), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, userDTOFrom(*u))
	}
}

// serviceTokenStatus loads the service-wide Claude token's summary, never
// returning the token itself.
func serviceTokenStatus(d *Deps, r *http.Request) (claudeTokenDTO, error) {
	_, _, label, verifiedAt, err := d.Tokens.GetService(r.Context())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return claudeTokenDTO{}, nil
		}
		return claudeTokenDTO{}, err
	}
	return claudeTokenDTO{Present: true, Label: label, VerifiedAt: verifiedAt}, nil
}

type settingsDTO struct {
	MaxOpenSessions int            `json:"max_open_sessions"`
	IdleTimeout     string         `json:"idle_timeout"`
	ServiceToken    claudeTokenDTO `json:"service_token"`
}

// handleSettingsGet is GET /api/v1/settings.
func handleSettingsGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, err := serviceTokenStatus(d, r)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, settingsDTO{
			MaxOpenSessions: d.MaxOpenSessions,
			IdleTimeout:     d.IdleTimeout.String(),
			ServiceToken:    tok,
		})
	}
}

// handleSettingsServiceTokenPut is PUT /api/v1/settings/service-token: same
// verify-then-store rule as the per-user Claude token.
func handleSettingsServiceTokenPut(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in tokenPutInput
		if err := decodeJSON(r, &in); err != nil || in.Token == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "token is required")
			return
		}
		if err := d.Verifier.Verify(r.Context(), in.Token); err != nil {
			slog.Warn("claude token verification failed", "scope", "service", "err", err)
			writeErrorCode(w, http.StatusUnprocessableEntity, "token_invalid", "the token could not be verified: "+strings.TrimPrefix(err.Error(), "claude: verify: "))
			return
		}
		ciphertext, nonce, err := d.Box.Seal([]byte(in.Token))
		if err != nil {
			WriteError(w, err)
			return
		}
		label := "…" + crypto.Suffix(in.Token)
		if err := d.Tokens.SetService(r.Context(), ciphertext, nonce, label); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
