package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/auth"
)

// registerAuthRoutes mounts the login flow under /api/v1/auth. Every route
// here is otherwise unauthenticated (you are not logged in yet when hitting
// most of them), except /auth/logout-all: it needs a signed-in Principal to
// know whose sessions to delete, so it adds auth.RequireUser for just that
// one route rather than moving the whole group behind it.
func registerAuthRoutes(r chi.Router, d *Deps) {
	r.Get("/auth/providers", handleAuthProviders(d))
	r.Get("/auth/login/{slug}", handleAuthLogin(d))
	r.Get("/auth/callback", handleAuthCallback(d))
	r.Post("/auth/logout", handleAuthLogout(d))
	r.With(auth.RequireUser).Post("/auth/logout-all", handleAuthLogoutAll(d))
}

type providerDTO struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// handleAuthProviders is GET /api/v1/auth/providers.
func handleAuthProviders(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers := d.Auth.Providers()
		out := make([]providerDTO, 0, len(providers))
		for _, p := range providers {
			out = append(out, providerDTO{Name: p.Name, Slug: p.Slug})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleAuthLogin is GET /api/v1/auth/login/{slug}: redirects to the
// provider's authorization endpoint.
func handleAuthLogin(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		redirectURL, err := d.Auth.BeginLogin(w, r, slug)
		if err != nil {
			http.Redirect(w, r, "/login?error=provider", http.StatusFound)
			return
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}

// handleAuthCallback is GET /api/v1/auth/callback: completes the OIDC
// exchange and redirects to "/" on success, "/login?error=..." on failure.
func handleAuthCallback(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := d.Auth.CompleteLogin(w, r); err != nil {
			http.Redirect(w, r, "/login?error=callback", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// handleAuthLogout is POST /api/v1/auth/logout.
func handleAuthLogout(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Auth.Logout(w, r); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleAuthLogoutAll is POST /api/v1/auth/logout-all: ends every browser
// session for the signed-in user, not just the one behind this request.
func handleAuthLogoutAll(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Auth.LogoutAll(w, r); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
