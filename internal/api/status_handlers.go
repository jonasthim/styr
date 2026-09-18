package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// registerStatusRoutes mounts the server status summary.
func registerStatusRoutes(r chi.Router, d *Deps) {
	r.Get("/status", handleStatus(d))
}

// handleStatus is GET /api/v1/status.
func handleStatus(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, d.Status())
	}
}
