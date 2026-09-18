package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// sseHeartbeatInterval is how often the SSE stream writes a comment-only
// ": ping" frame to keep intermediaries (and the browser) from timing the
// connection out.
const sseHeartbeatInterval = 25 * time.Second

// sseBufferSize is the per-subscriber channel buffer given to events.Bus.
const sseBufferSize = 256

// registerEventsRoute mounts GET /api/v1/events, the live SSE stream. It is
// registered on its own route group in router.go, outside
// middleware.Timeout, since it is intentionally long-lived.
func registerEventsRoute(r chi.Router, d *Deps) {
	r.Get("/events", handleEvents(d))
}

// handleEvents streams every bus message visible to the requesting user
// (owned by them, owner-less, or every message for an admin) as an SSE
// frame: "event: <Kind>\ndata: <Message JSON>\n\n".
func handleEvents(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeErrorCode(w, http.StatusInternalServerError, "internal", "streaming unsupported")
			return
		}

		actor := actorFrom(r)
		ch, unsubscribe := d.Bus.Subscribe(r.Context(), sseBufferSize)
		defer unsubscribe()

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no")
		h.Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		heartbeat := time.NewTicker(sseHeartbeatInterval)
		defer heartbeat.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if msg.OwnerID != nil && *msg.OwnerID != actor.UserID && !actor.IsAdmin {
					continue
				}
				payload, err := json.Marshal(msg)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Kind, payload); err != nil {
					return
				}
				flusher.Flush()
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
