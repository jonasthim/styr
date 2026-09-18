package api

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
)

// maxHookBodyBytes bounds an inbound POST /hooks/{slug} body. It is
// enforced with http.MaxBytesReader (a 413 on overflow), independent of
// maxJSONBody: a hook payload is not decoded as Styr's own JSON here at
// all — it is handed to TriggersService.Deliver as raw bytes, since its
// shape depends on the trigger's kind.
const maxHookBodyBytes = 256 * 1024 // 256 KiB

// registerHooksRoutes mounts POST /hooks/{slug}, the inbound webhook
// endpoint. It is deliberately registered on the root router in router.go,
// not inside the /api/v1 group: no session cookie, no CSRF header and no
// RequireUser — the caller authenticates with the trigger's own secret
// (or, for a "github" trigger, an HMAC signature), which
// TriggersService.Deliver checks using in.Headers.
func registerHooksRoutes(r chi.Router, d *Deps) {
	r.Post("/hooks/{slug}", handleHooksDeliver(d))
}

// handleHooksDeliver is POST /hooks/{slug}. Success is 202 (the delivery
// was logged; a run may or may not have started depending on dedupe,
// cooldown and the storm cap — see Status). Failures: 404 unknown/disabled
// trigger, 401 bad secret, 413 body over maxHookBodyBytes, 500 (no detail)
// for anything else.
func handleHooksDeliver(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")

		r.Body = http.MaxBytesReader(w, r.Body, maxHookBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeErrorCode(w, http.StatusRequestEntityTooLarge, "too_large", "request body exceeds the 256 KiB limit")
				return
			}
			writeErrorCode(w, http.StatusBadRequest, "invalid", "could not read request body")
			return
		}

		delivery, err := d.Triggers.Deliver(r.Context(), domain.Inbound{
			Slug: slug, Body: body, Headers: r.Header, Query: r.URL.Query(), Now: time.Now(),
		})
		if err != nil {
			writeHookError(w, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, deliveryResultDTOFrom(delivery))
	}
}

// writeHookError maps Deliver's sentinel errors to their documented status
// codes; anything else is a 500 with no error detail leaked to the caller.
func writeHookError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrUnknownTrigger):
		writeErrorCode(w, http.StatusNotFound, "not_found", "unknown or disabled trigger")
	case errors.Is(err, domain.ErrBadSecret):
		writeErrorCode(w, http.StatusUnauthorized, "unauthorized", "invalid secret")
	case errors.Is(err, domain.ErrTooLarge):
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "too_large", "request body exceeds the 256 KiB limit")
	default:
		writeErrorCode(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
