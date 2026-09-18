package api

import (
	"net/http"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/sessions"
)

// principal returns the request's authenticated Principal, if any.
func principal(r *http.Request) (*auth.Principal, bool) {
	return auth.PrincipalFrom(r.Context())
}

// actorFrom builds the sessions.Actor for the request's authenticated user.
// Handlers behind RequireUser can assume a principal is present; actorFrom
// returns the zero Actor when it is not (never true behind RequireUser).
func actorFrom(r *http.Request) sessions.Actor {
	p, ok := principal(r)
	if !ok {
		return sessions.Actor{}
	}
	return sessions.Actor{UserID: p.User.ID, IsAdmin: p.User.Role == domain.RoleAdmin}
}

// isAdmin reports whether the request's authenticated user is an admin.
func isAdmin(r *http.Request) bool {
	p, ok := principal(r)
	return ok && p.User.Role == domain.RoleAdmin
}
