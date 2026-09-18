// Package auth implements Styr's OIDC PKCE login, cookie-backed browser
// sessions and role checks, on top of the db.Users and db.LoginSessions
// repositories.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
)

// devIssuer is the synthetic issuer used for the dev-mode bypass user.
const devIssuer = "dev"

// Principal is the authenticated caller Authenticate attaches to a
// request's context.
type Principal struct {
	User           domain.User
	LoginSessionID string
}

// ProviderInfo describes one configured OIDC login option, as shown on the
// login page.
type ProviderInfo struct {
	Name string
	Slug string
}

// Service implements OIDC PKCE login, cookie sessions and role checks.
type Service struct {
	users     *db.Users
	logins    *db.LoginSessions
	providers []config.OIDCProvider
	baseURL   string
	secure    bool
	devUser   string

	mu       sync.Mutex
	runtimes map[string]*providerRuntime // provider slug -> lazily discovered

	// firstUserMu serializes the get-then-create (and, in upsertUser,
	// count-then-create) races around minting a new user row: two
	// concurrent first logins must not both decide they are user #0 and
	// both become admin, and two concurrent dev-mode requests must not
	// both try to create the dev bypass user. It guards only that narrow
	// section, never the OIDC discovery/runtime cache above.
	firstUserMu sync.Mutex
}

// New constructs the auth service. providers is the configured OIDC login
// options (may be empty). baseURL builds the OIDC redirect URI (baseURL +
// "/api/v1/auth/callback"); it should not have a trailing slash, but New
// trims one if present. secure controls the Secure flag on both cookies.
// devUser, when non-empty, enables the dev bypass in Authenticate; New
// itself does not validate devUser against providers or the environment —
// config.Config.Validate is where prod-mode rules are enforced.
//
// OIDC provider discovery (oidc.NewProvider, a network call) is not
// performed here: it happens lazily, on first BeginLogin/CompleteLogin for
// a given provider slug, and the result is cached for the life of the
// Service (see runtimeFor in oidc.go). That keeps New free of I/O and lets
// tests point a provider's issuer at an httptest server with no extra
// plumbing — discovery uses http.DefaultClient, which talks to the
// httptest server directly over plain HTTP.
func New(users *db.Users, logins *db.LoginSessions, providers []config.OIDCProvider, baseURL string, secure bool, devUser string) (*Service, error) {
	if users == nil || logins == nil {
		return nil, errors.New("auth: users and logins repositories are required")
	}
	seen := make(map[string]string, len(providers))
	for _, p := range providers {
		slug := slugify(p.Name)
		if other, ok := seen[slug]; ok && other != p.Name {
			return nil, fmt.Errorf("auth: providers %q and %q both slugify to %q", other, p.Name, slug)
		}
		seen[slug] = p.Name
	}
	return &Service{
		users:     users,
		logins:    logins,
		providers: providers,
		baseURL:   strings.TrimRight(baseURL, "/"),
		secure:    secure,
		devUser:   devUser,
		runtimes:  make(map[string]*providerRuntime),
	}, nil
}

// Providers lists the configured OIDC login options for the login page.
func (s *Service) Providers() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(s.providers))
	for _, p := range s.providers {
		out = append(out, ProviderInfo{Name: p.Name, Slug: slugify(p.Name)})
	}
	return out
}

// slugify lowercases name and replaces every character outside [a-z0-9]
// with "-", one-for-one (no collapsing of consecutive replacements).
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// providerConfig returns the configured provider whose name slugifies to
// slug.
func (s *Service) providerConfig(slug string) (config.OIDCProvider, bool) {
	for _, p := range s.providers {
		if slugify(p.Name) == slug {
			return p, true
		}
	}
	return config.OIDCProvider{}, false
}

// ---- session cookie lifecycle ---------------------------------------------

// createSession mints a new random session token, stores its hash via
// db.LoginSessions.Create and sets the styr_session cookie.
func (s *Service) createSession(ctx context.Context, w http.ResponseWriter, userID string, r *http.Request) (id string, err error) {
	raw, err := randToken(32)
	if err != nil {
		return "", err
	}
	hash := hashToken(raw)
	now := time.Now()
	ua := ""
	if r != nil {
		ua = r.UserAgent()
	}
	id, err = s.logins.Create(ctx, userID, hash, now.Add(sessionCookieTTL), ua)
	if err != nil {
		return "", fmt.Errorf("create login session: %w", err)
	}

	setSessionCookie(w, s.secure, raw, now.Add(sessionCookieTTL))
	return id, nil
}

// principalFromCookie resolves the styr_session cookie carried by r, if
// any, into a Principal. db.LoginSessions.GetByHash returns the row's own
// id and last_seen_at, so Touch and (in Logout) Delete always target the
// real row straight from the database — no in-memory state to lose across a
// process restart.
func (s *Service) principalFromCookie(ctx context.Context, r *http.Request) (*Principal, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil, false
	}
	hash := hashToken(c.Value)
	id, userID, expires, lastSeen, err := s.logins.GetByHash(ctx, hash)
	if err != nil {
		return nil, false
	}
	now := time.Now()
	if now.After(expires) {
		return nil, false
	}
	usr, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, false
	}
	if now.Sub(lastSeen) > touchInterval {
		_ = s.logins.Touch(ctx, id)
	}
	return &Principal{User: *usr, LoginSessionID: id}, true
}

// devPrincipal loads or creates the dev-mode bypass user and starts a real
// session for it, so subsequent requests authenticate via the normal cookie
// path. The dev user always gets RoleAdmin, regardless of how many users
// already exist ("first user is admin" only applies to real OIDC logins).
func (s *Service) devPrincipal(ctx context.Context, w http.ResponseWriter, r *http.Request) (*Principal, bool) {
	usr, err := s.users.GetBySubject(ctx, devIssuer, s.devUser)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, false
		}
		created, ok := s.getOrCreateDevUser(ctx)
		if !ok {
			return nil, false
		}
		usr = created
	} else {
		_ = s.users.TouchLogin(ctx, usr.ID)
	}
	id, err := s.createSession(ctx, w, usr.ID, r)
	if err != nil {
		return nil, false
	}
	return &Principal{User: *usr, LoginSessionID: id}, true
}

// getOrCreateDevUser loads or creates the dev-mode bypass user under
// firstUserMu, so two concurrent requests racing to bootstrap it in dev mode
// (no cookie yet on either) cannot both observe ErrNotFound and both insert
// a row, which would otherwise fail with a unique-constraint error for one
// of them.
func (s *Service) getOrCreateDevUser(ctx context.Context) (*domain.User, bool) {
	s.firstUserMu.Lock()
	defer s.firstUserMu.Unlock()

	if existing, err := s.users.GetBySubject(ctx, devIssuer, s.devUser); err == nil {
		return existing, true
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, false
	}

	now := time.Now()
	created := domain.User{
		ID: uuid.NewString(), Issuer: devIssuer, Subject: s.devUser,
		Email: s.devUser, DisplayName: "Dev User", Role: domain.RoleAdmin,
		CreatedAt: now, LastLoginAt: now, Prefs: json.RawMessage(`{}`),
	}
	if err := s.users.Create(ctx, created); err != nil {
		return nil, false
	}
	return &created, true
}

// Authenticate resolves the styr_session cookie (or, in dev mode with no
// cookie, the dev bypass user) and attaches a *Principal to the request
// context. It never rejects a request itself: RequireUser and RequireAdmin
// do that once a principal is actually required.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if p, ok := s.principalFromCookie(ctx, r); ok {
			next.ServeHTTP(w, r.WithContext(withPrincipal(ctx, p)))
			return
		}
		if s.devUser != "" {
			if p, ok := s.devPrincipal(ctx, w, r); ok {
				next.ServeHTTP(w, r.WithContext(withPrincipal(ctx, p)))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Logout clears the styr_session cookie and, when the request carried a
// valid one, deletes the corresponding login_sessions row. The row id comes
// from db.LoginSessions.GetByHash, so this works even for a cookie whose
// session was created by a different *Service instance (e.g. before a
// process restart) — nothing about the delete depends on in-memory state.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) error {
	defer clearSessionCookie(w, s.secure)
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	hash := hashToken(c.Value)
	id, _, _, _, err := s.logins.GetByHash(r.Context(), hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("logout: %w", err)
	}
	if err := s.logins.Delete(r.Context(), id); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// LogoutAll clears the styr_session cookie and deletes every login_sessions
// row belonging to the signed-in caller, ending every browser session for
// that user — not just the one behind this request's own cookie. Unlike
// Logout, it needs a Principal (the target of "every session" is the
// caller, not whoever a cookie happens to name), so it must run behind
// RequireUser; called without one it returns an error and deletes nothing.
func (s *Service) LogoutAll(w http.ResponseWriter, r *http.Request) error {
	defer clearSessionCookie(w, s.secure)
	p, ok := PrincipalFrom(r.Context())
	if !ok {
		return errors.New("logout-all: no authenticated principal")
	}
	if err := s.logins.DeleteAllForUser(r.Context(), p.User.ID); err != nil {
		return fmt.Errorf("logout-all: %w", err)
	}
	return nil
}

// ---- context / role checks --------------------------------------------------

type principalCtxKey struct{}

func withPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, p)
}

// PrincipalFrom returns the Principal Authenticate attached to ctx, if any.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(*Principal)
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}

// RequireUser answers 401 when Authenticate did not attach a Principal to
// the request.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFrom(r.Context()); !ok {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin answers 401 (no principal) or 403 (principal is not an
// admin).
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFrom(r.Context())
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		if p.User.Role != domain.RoleAdmin {
			writeAuthError(w, http.StatusForbidden, "forbidden", "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
