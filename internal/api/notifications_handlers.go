package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/notify"
)

// defaultChannelEvents mirrors the notification_channels table's default
// events column, used when a caller creates a channel without naming any.
var defaultChannelEvents = []string{"run.finished", "run.needs_human", "run.failed"}

// allowedChannelKinds are the notification channel kinds Styr accepts.
var allowedChannelKinds = map[string]bool{"ntfy": true, "webhook": true}

// registerNotificationsRoutes mounts notification channel CRUD and test
// routes. Unlike templates/triggers these channels have no per-user
// ownership (they are a shared, server-wide setting, like profiles), so
// every mutating route is admin-only; any signed-in user may list them.
func registerNotificationsRoutes(r chi.Router, d *Deps) {
	r.Get("/notifications", handleNotificationsList(d))
	r.With(auth.RequireAdmin).Post("/notifications", handleNotificationsCreate(d))
	r.With(auth.RequireAdmin).Patch("/notifications/{id}", handleNotificationsPatch(d))
	r.With(auth.RequireAdmin).Delete("/notifications/{id}", handleNotificationsDelete(d))
	r.With(auth.RequireAdmin).Post("/notifications/{id}/test", handleNotificationsTest(d))
}

// notificationDTO never carries the token — only whether one is set.
type notificationDTO struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	TokenPresent bool      `json:"token_present"`
	Events       []string  `json:"events"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

func notificationDTOFrom(c domain.NotificationChannel) notificationDTO {
	events := c.Events
	if events == nil {
		events = []string{}
	}
	return notificationDTO{
		ID: c.ID, Kind: string(c.Kind), Name: c.Name, URL: c.URL,
		TokenPresent: len(c.TokenCiphertext) > 0, Events: events, Enabled: c.Enabled, CreatedAt: c.CreatedAt,
	}
}

// handleNotificationsList is GET /api/v1/notifications.
func handleNotificationsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Notifications.List(r.Context())
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]notificationDTO, 0, len(list))
		for _, c := range list {
			out = append(out, notificationDTOFrom(c))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type notificationCreateInput struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Token  string   `json:"token"`
	Events []string `json:"events"`
}

// handleNotificationsCreate is POST /api/v1/notifications.
func handleNotificationsCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in notificationCreateInput
		if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.URL == "" || !allowedChannelKinds[in.Kind] {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "kind (ntfy or webhook), name and url are required")
			return
		}
		events := in.Events
		if len(events) == 0 {
			events = defaultChannelEvents
		}
		c := domain.NotificationChannel{
			ID: uuid.NewString(), Kind: domain.ChannelKind(in.Kind), Name: in.Name, URL: in.URL,
			Events: events, Enabled: true, CreatedAt: time.Now(),
		}
		if in.Token != "" {
			ciphertext, nonce, err := d.Box.Seal([]byte(in.Token))
			if err != nil {
				WriteError(w, err)
				return
			}
			c.TokenCiphertext, c.TokenNonce = ciphertext, nonce
		}
		if err := d.Notifications.Create(r.Context(), c); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, notificationDTOFrom(c))
	}
}

type notificationPatchInput struct {
	Kind    *string   `json:"kind"`
	Name    *string   `json:"name"`
	URL     *string   `json:"url"`
	Token   *string   `json:"token"`
	Events  *[]string `json:"events"`
	Enabled *bool     `json:"enabled"`
}

// handleNotificationsPatch is PATCH /api/v1/notifications/{id}. A present
// but empty token clears it; an absent token field leaves it unchanged.
func handleNotificationsPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := d.Notifications.Get(r.Context(), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		var in notificationPatchInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if in.Kind != nil {
			if !allowedChannelKinds[*in.Kind] {
				writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "kind must be ntfy or webhook")
				return
			}
			c.Kind = domain.ChannelKind(*in.Kind)
		}
		if in.Name != nil {
			c.Name = *in.Name
		}
		if in.URL != nil {
			c.URL = *in.URL
		}
		if in.Events != nil {
			c.Events = *in.Events
		}
		if in.Enabled != nil {
			c.Enabled = *in.Enabled
		}
		if in.Token != nil {
			if *in.Token == "" {
				c.TokenCiphertext, c.TokenNonce = nil, nil
			} else {
				ciphertext, nonce, err := d.Box.Seal([]byte(*in.Token))
				if err != nil {
					WriteError(w, err)
					return
				}
				c.TokenCiphertext, c.TokenNonce = ciphertext, nonce
			}
		}
		if err := d.Notifications.Update(r.Context(), *c); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, notificationDTOFrom(*c))
	}
}

// handleNotificationsDelete is DELETE /api/v1/notifications/{id}.
func handleNotificationsDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Notifications.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleNotificationsTest is POST /api/v1/notifications/{id}/test: sends a
// synthetic "test" event through the channel using its decrypted token, so
// an operator can confirm a channel is configured correctly. The token
// never appears in the response either way.
func handleNotificationsTest(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := d.Notifications.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		var token string
		if len(c.TokenCiphertext) > 0 {
			plain, err := d.Box.Open(c.TokenCiphertext, c.TokenNonce)
			if err != nil {
				WriteError(w, err)
				return
			}
			token = string(plain)
		}
		ch := notify.Channel{ID: c.ID, Kind: string(c.Kind), Name: c.Name, URL: c.URL, Token: token, Events: c.Events}
		ev := notify.Event{Kind: "test", Title: "Styr test notification", Body: "This is a test notification from Styr.", Priority: 3}
		if err := d.Notifier.Send(r.Context(), ch, ev); err != nil {
			writeErrorCode(w, http.StatusBadGateway, "notify_failed", "sending the test notification failed")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
