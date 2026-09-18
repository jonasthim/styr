package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
)

// testOpenDB opens a fresh, migrated sqlite database in a temp dir. auth's
// own tests cannot reach db's unexported testOpenDB helper, so this is a
// small local equivalent built on the exported db.Open.
func testOpenDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "styr.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func newTestRepos(t *testing.T) (*db.Users, *db.LoginSessions) {
	t.Helper()
	d := testOpenDB(t)
	return db.NewUsers(d), db.NewLoginSessions(d)
}

func TestProviders_SlugifiesNames(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, []config.OIDCProvider{
		{Name: "Authentik"},
		{Name: "My Company IdP"},
		{Name: "Google (SSO)!"},
	}, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := svc.Providers()
	want := []ProviderInfo{
		{Name: "Authentik", Slug: "authentik"},
		{Name: "My Company IdP", Slug: "my-company-idp"},
		{Name: "Google (SSO)!", Slug: "google--sso--"},
	}
	if len(got) != len(want) {
		t.Fatalf("Providers() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Providers()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestNew_RejectsNilRepos(t *testing.T) {
	if _, err := New(nil, nil, nil, "http://localhost:8080", false, ""); err == nil {
		t.Fatal("New with nil repositories: want error, got nil")
	}
}

// seedSession creates a user directly via the repository and a real session
// via the unexported createSession, so Authenticate/Logout can be tested
// without running a full OIDC exchange.
func seedSession(t *testing.T, svc *Service, users *db.Users, role domain.Role) (*domain.User, *http.Cookie) {
	t.Helper()
	ctx := t.Context()
	usr := newTestDomainUser(role)
	if err := users.Create(ctx, usr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := svc.createSession(ctx, rec, usr.ID, req); err != nil {
		t.Fatalf("createSession: %v", err)
	}
	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			return &usr, c
		}
	}
	t.Fatal("createSession did not set the session cookie")
	return nil, nil
}

func TestAuthenticate_SetsPrincipalFromCookie(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	usr, cookie := seedSession(t, svc, users, domain.RoleMember)

	var gotPrincipal *Principal
	handler := svc.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal, _ = PrincipalFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotPrincipal == nil {
		t.Fatal("expected a principal in context, got none")
	}
	if gotPrincipal.User.ID != usr.ID {
		t.Errorf("principal user id = %q, want %q", gotPrincipal.User.ID, usr.ID)
	}
	if gotPrincipal.LoginSessionID == "" {
		t.Error("expected a non-empty LoginSessionID")
	}
}

func TestAuthenticate_NoCookieNoDevUser_LeavesRequestUnauthenticated(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var ok bool
	handler := svc.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok = PrincipalFrom(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if ok {
		t.Error("expected no principal without a cookie or dev user configured")
	}
}

func TestAuthenticate_DevBypass_OnlyWhenDevUserSet(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "alice")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var principal *Principal
	var sessionCookieSet bool
	handler := svc.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ = PrincipalFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if principal == nil {
		t.Fatal("expected a dev principal, got none")
	}
	if principal.User.Issuer != devIssuer || principal.User.Subject != "alice" {
		t.Errorf("dev user issuer/subject = %q/%q, want %q/%q", principal.User.Issuer, principal.User.Subject, devIssuer, "alice")
	}
	if principal.User.Role != domain.RoleAdmin {
		t.Errorf("dev user role = %q, want admin", principal.User.Role)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			sessionCookieSet = true
		}
	}
	if !sessionCookieSet {
		t.Error("expected the dev bypass to set a real session cookie")
	}

	// A second request without a cookie should reuse the same dev user
	// rather than creating a duplicate.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if principal.User.ID == "" {
		t.Fatal("expected a user id")
	}

	all, err := users.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	devCount := 0
	for _, u := range all {
		if u.Issuer == devIssuer {
			devCount++
		}
	}
	if devCount != 1 {
		t.Errorf("dev users in db = %d, want 1 (no duplicates across requests)", devCount)
	}
}

func TestAuthenticate_NoDevBypassWhenDevUserEmpty(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var ok bool
	handler := svc.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok = PrincipalFrom(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if ok {
		t.Error("expected no dev bypass when devUser is empty")
	}
}

func TestLogout_DeletesTheLoginSession(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, cookie := seedSession(t, svc, users, domain.RoleMember)

	hash := hashToken(cookie.Value)
	if _, _, _, _, err := logins.GetByHash(t.Context(), hash); err != nil {
		t.Fatalf("precondition: session should exist: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	if err := svc.Logout(rec, req); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if _, _, _, _, err := logins.GetByHash(t.Context(), hash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash after logout = %v, want ErrNotFound", err)
	}

	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected Logout to clear the session cookie")
	}
}

// TestLogout_DeletesSessionCreatedByADifferentServiceInstance simulates a
// process restart: svc1 creates the session (and, before this fix, would
// have been the only *Service instance that ever knew the row's real id,
// since that id lived only in its own in-memory idByHash map). svc2 is a
// fresh *Service sharing the same repositories/db but none of svc1's
// process state, exactly like the server after a restart. Logout on svc2
// must still find and delete the row, because the id now always comes from
// db.LoginSessions.GetByHash.
func TestLogout_DeletesSessionCreatedByADifferentServiceInstance(t *testing.T) {
	users, logins := newTestRepos(t)
	svc1, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New svc1: %v", err)
	}
	_, cookie := seedSession(t, svc1, users, domain.RoleMember)

	// A different *Service instance, sharing the same repositories, standing
	// in for the server after a restart.
	svc2, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New svc2: %v", err)
	}

	hash := hashToken(cookie.Value)
	if _, _, _, _, err := logins.GetByHash(t.Context(), hash); err != nil {
		t.Fatalf("precondition: session should exist: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	if err := svc2.Logout(rec, req); err != nil {
		t.Fatalf("Logout on a different Service instance: %v", err)
	}

	if _, _, _, _, err := logins.GetByHash(t.Context(), hash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash after logout from a different Service instance = %v, want ErrNotFound", err)
	}
}

func TestLogout_NoCookie_IsANoOp(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	if err := svc.Logout(httptest.NewRecorder(), req); err != nil {
		t.Fatalf("Logout with no cookie: %v", err)
	}
}

// TestLogoutAll_DeletesEveryLoginSessionForUser seeds two login sessions for
// the same user (as if signed in on two devices), calls LogoutAll with a
// request carrying only the first session's cookie and Principal, and
// checks that both hashes are gone from db.LoginSessions afterwards.
func TestLogoutAll_DeletesEveryLoginSessionForUser(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	usr, cookie1 := seedSession(t, svc, users, domain.RoleMember)

	// A second session for the same user, as if signed in on another
	// device/browser.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := svc.createSession(t.Context(), rec2, usr.ID, req2); err != nil {
		t.Fatalf("createSession (second session): %v", err)
	}
	var cookie2 *http.Cookie
	for _, c := range rec2.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie2 = c
		}
	}
	if cookie2 == nil {
		t.Fatal("second createSession did not set the session cookie")
	}

	hash1 := hashToken(cookie1.Value)
	hash2 := hashToken(cookie2.Value)
	if _, _, _, _, err := logins.GetByHash(t.Context(), hash1); err != nil {
		t.Fatalf("precondition: first session should exist: %v", err)
	}
	if _, _, _, _, err := logins.GetByHash(t.Context(), hash2); err != nil {
		t.Fatalf("precondition: second session should exist: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-all", nil)
	req.AddCookie(cookie1)
	req = req.WithContext(withPrincipal(req.Context(), &Principal{User: *usr}))
	rec := httptest.NewRecorder()
	if err := svc.LogoutAll(rec, req); err != nil {
		t.Fatalf("LogoutAll: %v", err)
	}

	if _, _, _, _, err := logins.GetByHash(t.Context(), hash1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("first session after LogoutAll = %v, want ErrNotFound", err)
	}
	if _, _, _, _, err := logins.GetByHash(t.Context(), hash2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second session after LogoutAll = %v, want ErrNotFound", err)
	}

	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected LogoutAll to clear the session cookie")
	}
}

// TestLogoutAll_NoPrincipal_ReturnsError documents that LogoutAll refuses to
// guess whose sessions to delete: without a Principal in the request
// context (RequireUser normally guarantees one) it errors instead of
// silently doing nothing or deleting the wrong user's rows.
func TestLogoutAll_NoPrincipal_ReturnsError(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-all", nil)
	if err := svc.LogoutAll(httptest.NewRecorder(), req); err == nil {
		t.Fatal("expected an error with no principal in context")
	}
}

func TestRequireUser_RejectsWithoutPrincipal(t *testing.T) {
	handler := RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run")
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAdmin_RejectsMember(t *testing.T) {
	member := domain.User{ID: "u1", Role: domain.RoleMember}
	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withPrincipal(req.Context(), &Principal{User: member}))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	admin := domain.User{ID: "u1", Role: domain.RoleAdmin}
	var ran bool
	handler := RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withPrincipal(req.Context(), &Principal{User: admin}))
	handler.ServeHTTP(rec, req)
	if !ran {
		t.Error("expected the next handler to run for an admin principal")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func newTestDomainUser(role domain.Role) domain.User {
	id := "usr-" + string(role)
	now := time.Now()
	return domain.User{
		ID: id, Issuer: "https://issuer.example", Subject: id,
		Email: id + "@example.com", DisplayName: "Test User", Role: role,
		CreatedAt: now, LastLoginAt: now,
	}
}
