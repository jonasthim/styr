package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/domain"
)

// maxAPITokenExpiryDays bounds POST /me/api-tokens's expires_in_days, same
// order of magnitude as valheim-server-ui's personal API tokens (~10
// years); 0 (or omitted) means the token never expires.
const maxAPITokenExpiryDays = 3650

// registerAPITokensRoutes mounts the current user's own personal API
// tokens: GET/POST /me/api-tokens and DELETE /me/api-tokens/{id}. Every
// handler here answers 501 when d.TokenStore is nil (T31's db.APITokens
// repository not yet wired in cmd/styr/wire.go), rather than reaching for a
// nil interface.
func registerAPITokensRoutes(r chi.Router, d *Deps) {
	r.Get("/me/api-tokens", handleAPITokensList(d))
	r.Post("/me/api-tokens", handleAPITokensCreate(d))
	r.Delete("/me/api-tokens/{id}", handleAPITokensDelete(d))
}

func tokenStoreUnavailable(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "api tokens are not available")
}

// apiTokenDTO is what GET and POST /me/api-tokens return for each token:
// never TokenHash, and only POST's create response (apiTokenCreatedDTO)
// ever carries the raw secret.
type apiTokenDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

func apiTokenDTOFrom(t domain.APIToken) apiTokenDTO {
	return apiTokenDTO{
		ID: t.ID, Name: t.Name, Prefix: t.Prefix,
		CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, ExpiresAt: t.ExpiresAt,
	}
}

// handleAPITokensList is GET /api/v1/me/api-tokens.
func handleAPITokensList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.TokenStore == nil {
			tokenStoreUnavailable(w)
			return
		}
		p, _ := principal(r)
		list, err := d.TokenStore.ListByUser(r.Context(), p.User.ID)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]apiTokenDTO, 0, len(list))
		for _, t := range list {
			out = append(out, apiTokenDTOFrom(t))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type apiTokenCreateInput struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expires_in_days"`
}

// apiTokenCreatedDTO is POST /me/api-tokens's 201 response: the only place
// the raw token is ever returned.
type apiTokenCreatedDTO struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Token  string `json:"token"`
}

// handleAPITokensCreate is POST /api/v1/me/api-tokens: {name,
// expires_in_days?} -> 201 with the raw token, shown here once and never
// again (only its sha256 hash is stored, via d.TokenStore.Create).
func handleAPITokensCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.TokenStore == nil {
			tokenStoreUnavailable(w)
			return
		}
		var in apiTokenCreateInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "name is required")
			return
		}
		if in.ExpiresInDays != nil && (*in.ExpiresInDays < 0 || *in.ExpiresInDays > maxAPITokenExpiryDays) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "expires_in_days must be 0 (never) or between 1 and 3650")
			return
		}

		raw, hash, prefix, err := auth.GenerateAPIToken()
		if err != nil {
			WriteError(w, err)
			return
		}

		p, _ := principal(r)
		now := time.Now()
		var expiresAt *time.Time
		if in.ExpiresInDays != nil && *in.ExpiresInDays > 0 {
			t := now.AddDate(0, 0, *in.ExpiresInDays)
			expiresAt = &t
		}
		tok := domain.APIToken{
			ID: uuid.NewString(), UserID: p.User.ID, Name: name,
			TokenHash: hash, Prefix: prefix, CreatedAt: now, ExpiresAt: expiresAt,
		}
		if err := d.TokenStore.Create(r.Context(), tok); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, apiTokenCreatedDTO{ID: tok.ID, Name: tok.Name, Prefix: tok.Prefix, Token: raw})
	}
}

// handleAPITokensDelete is DELETE /api/v1/me/api-tokens/{id}: only the
// caller's own tokens (d.TokenStore.Delete scopes by userID); any other id,
// including one belonging to another user, answers 404 via
// domain.ErrNotFound.
func handleAPITokensDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.TokenStore == nil {
			tokenStoreUnavailable(w)
			return
		}
		p, _ := principal(r)
		id := chi.URLParam(r, "id")
		if err := d.TokenStore.Delete(r.Context(), id, p.User.ID); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
