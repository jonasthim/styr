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
	if _, _, err := logins.GetByHash(t.Context(), hash); err != nil {
		t.Fatalf("precondition: session should exist: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	if err := svc.Logout(rec, req); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if _, _, err := logins.GetByHash(t.Context(), hash); !errors.Is(err, domain.ErrNotFound) {
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
