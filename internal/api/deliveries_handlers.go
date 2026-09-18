package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
)

// registerDeliveriesRoutes mounts the top-level delivery route: replay is
// addressed by delivery id, not nested under its trigger.
func registerDeliveriesRoutes(r chi.Router, d *Deps) {
	r.Post("/deliveries/{id}/replay", handleDeliveriesReplay(d))
}

// deliveryDTO is shared with triggers_handlers.go's GET
// /triggers/{id}/deliveries, which lists deliveries of the same shape.
type deliveryDTO struct {
	ID         string          `json:"id"`
	TriggerID  string          `json:"trigger_id"`
	ReceivedAt time.Time       `json:"received_at"`
	Status     string          `json:"status"`
	Reason     string          `json:"reason"`
	DedupeKey  string          `json:"dedupe_key"`
	Payload    json.RawMessage `json:"payload"`
	RunID      *string         `json:"run_id"`
}

func deliveryDTOFrom(dl domain.Delivery) deliveryDTO {
	return deliveryDTO{
		ID: dl.ID, TriggerID: dl.TriggerID, ReceivedAt: dl.ReceivedAt, Status: string(dl.Status),
		Reason: dl.Reason, DedupeKey: dl.DedupeKey, Payload: dl.Payload, RunID: dl.RunID,
	}
}

type deliveryReplayDTO struct {
	RunID *string `json:"run_id,omitempty"`
}

// deliveryResultDTO is the shape both POST /hooks/{slug} and POST
// /triggers/{id}/test respond with: enough to look the delivery up (or
// follow it to the run it started) without the full Delivery record.
type deliveryResultDTO struct {
	DeliveryID string  `json:"delivery_id"`
	Status     string  `json:"status"`
	RunID      *string `json:"run_id,omitempty"`
}

func deliveryResultDTOFrom(dl domain.Delivery) deliveryResultDTO {
	return deliveryResultDTO{DeliveryID: dl.ID, Status: string(dl.Status), RunID: dl.RunID}
}

// handleDeliveriesReplay is POST /api/v1/deliveries/{id}/replay: 202
// {run_id?} — re-renders the delivery's stored payload and starts a new
// run, bypassing dedupe (a deliberate re-run of something already seen).
func handleDeliveriesReplay(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dl, err := d.Triggers.Replay(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, deliveryReplayDTO{RunID: dl.RunID})
	}
}
