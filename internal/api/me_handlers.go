package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/domain"
)

// registerMeRoutes mounts the current user's own profile and Claude token.
func registerMeRoutes(r chi.Router, d *Deps) {
	r.Get("/me", handleMeGet(d))
	r.Patch("/me", handleMePatch(d))
	r.Put("/me/claude-token", handleMeTokenPut(d))
	r.Delete("/me/claude-token", handleMeTokenDelete(d))
}

type claudeTokenDTO struct {
	Present    bool       `json:"present"`
	Label      string     `json:"label"`
	VerifiedAt *time.Time `json:"verified_at"`
}

type meDTO struct {
	ID          string          `json:"id"`
	Email       string          `json:"email"`
	DisplayName string          `json:"display_name"`
	AvatarURL   string          `json:"avatar_url"`
	Role        string          `json:"role"`
	Prefs       json.RawMessage `json:"prefs"`
	ClaudeToken claudeTokenDTO  `json:"claude_token"`
}

// claudeTokenFor loads the claude-token summary for userID, never
// returning the token itself.
func claudeTokenFor(d *Deps, r *http.Request, userID string) (claudeTokenDTO, error) {
	_, _, label, verifiedAt, err := d.Tokens.Get(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return claudeTokenDTO{}, nil
		}
		return claudeTokenDTO{}, err
	}
	return claudeTokenDTO{Present: true, Label: label, VerifiedAt: verifiedAt}, nil
}

func meDTOFor(d *Deps, r *http.Request, u domain.User) (meDTO, error) {
	tok, err := claudeTokenFor(d, r, u.ID)
	if err != nil {
		return meDTO{}, err
	}
	return meDTO{
		ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, AvatarURL: u.AvatarURL,
		Role: string(u.Role), Prefs: u.Prefs, ClaudeToken: tok,
	}, nil
}

// handleMeGet is GET /api/v1/me.
func handleMeGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		dto, err := meDTOFor(d, r, p.User)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, dto)
	}
}

type mePatchInput struct {
	Prefs json.RawMessage `json:"prefs"`
}

// handleMePatch is PATCH /api/v1/me: currently only prefs are editable.
func handleMePatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		var in mePatchInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if len(in.Prefs) > 0 {
			if err := d.Users.UpdatePrefs(r.Context(), p.User.ID, in.Prefs); err != nil {
				WriteError(w, err)
				return
			}
			p.User.Prefs = in.Prefs
		}
		dto, err := meDTOFor(d, r, p.User)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, dto)
	}
}

type tokenPutInput struct {
	Token string `json:"token"`
}

// handleMeTokenPut is PUT /api/v1/me/claude-token: verifies the token
// before storing anything, and never echoes it back.
func handleMeTokenPut(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		var in tokenPutInput
		if err := decodeJSON(r, &in); err != nil || in.Token == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "token is required")
			return
		}
		if err := d.Verifier.Verify(r.Context(), in.Token); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "token_invalid", "the token could not be verified")
			return
		}
		ciphertext, nonce, err := d.Box.Seal([]byte(in.Token))
		if err != nil {
			WriteError(w, err)
			return
		}
		label := "…" + crypto.Suffix(in.Token)
		if err := d.Tokens.Set(r.Context(), p.User.ID, ciphertext, nonce, label); err != nil {
			WriteError(w, err)
			return
		}
		if err := d.Tokens.MarkVerified(r.Context(), p.User.ID); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMeTokenDelete is DELETE /api/v1/me/claude-token. Deleting an
// already-absent token is not an error.
func handleMeTokenDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		if err := d.Tokens.Delete(r.Context(), p.User.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
