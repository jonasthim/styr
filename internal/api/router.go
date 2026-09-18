package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jonasthim/styr/internal/auth"
)

// requestTimeout bounds every /api/v1 request except the SSE stream, which
// is intentionally long-lived.
const requestTimeout = 5 * time.Minute

// NewRouter builds Styr's full HTTP handler: /healthz, /api/v1 and, when
// spa is non-nil, the embedded single-page app as the fallback route (spa
// is produced by the web package; this package never builds one itself, so
// it does not import internal/api/spa.go — there is no such file).
func NewRouter(d *Deps, spa http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(realIP)
	r.Use(securityHeaders)
	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)

	// GET /healthz is unauthenticated and outside /api/v1 entirely, so
	// neither Authenticate nor csrfGuard nor the timeout ever run for it.
	r.Get("/healthz", healthzHandler)

	// POST /hooks/{slug} is the inbound webhook endpoint: unauthenticated
	// by cookie (the trigger's own secret is the credential) and outside
	// csrfGuard and the request timeout, same as /healthz.
	registerHooksRoutes(r, d)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(d.Auth.Authenticate)

		// Auth routes are outside RequireUser (you are not logged in yet
		// when hitting them) but still get the CSRF guard and the request
		// timeout, matching every other /api/v1 route.
		api.Group(func(g chi.Router) {
			g.Use(middleware.Timeout(requestTimeout))
			g.Use(csrfGuard)
			registerAuthRoutes(g, d)
		})

		// Every other route requires a signed-in user.
		api.Group(func(g chi.Router) {
			g.Use(auth.RequireUser)
			g.Use(csrfGuard)
			g.Use(middleware.Timeout(requestTimeout))

			registerMeRoutes(g, d)
			registerAPITokensRoutes(g, d)
			registerUsersRoutes(g, d)
			registerWorkspacesRoutes(g, d)
			registerProfilesRoutes(g, d)
			registerSessionsRoutes(g, d)
			registerReviewRoutes(g, d)
			registerApprovalsRoutes(g, d)
			registerStatusRoutes(g, d)
			registerTemplatesRoutes(g, d)
			registerTriggersRoutes(g, d)
			registerDeliveriesRoutes(g, d)
			registerRunsRoutes(g, d)
			registerNotificationsRoutes(g, d)
			registerSchedulesRoutes(g, d)
			registerLoopsRoutes(g, d)
			registerStatsRoutes(g, d)
		})

		// The SSE stream requires a signed-in user too, but must not be cut
		// off by the request timeout.
		api.Group(func(g chi.Router) {
			g.Use(auth.RequireUser)
			registerEventsRoute(g, d)
		})

		api.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeErrorCode(w, http.StatusNotFound, "not_found", "route not found")
		})
	})

	if spa != nil {
		r.Handle("/*", spa)
	}
	return r
}

// healthzHandler is GET /healthz: an unauthenticated liveness check.
func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
