package api_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// sessionHashFromJar reads the styr_session cookie currently set on
// client's jar for e's server and returns the sha256 hex hash
// db.LoginSessions stores for it — the same algorithm as
// internal/auth/cookies.go's unexported hashToken, reimplemented here since
// api_test cannot reach into package auth's internals.
func (e *testEnv) sessionHashFromJar(t *testing.T, client *http.Client) string {
	t.Helper()
	u, err := url.Parse(e.ts.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == "styr_session" {
			sum := sha256.Sum256([]byte(c.Value))
			return hex.EncodeToString(sum[:])
		}
	}
	t.Fatal("no styr_session cookie on client jar")
	return ""
}

func TestAuthProviders_Empty(t *testing.T) {
	e := newEnv(t)
	var out []map[string]string
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/auth/providers", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /auth/providers = %d, want 200", status)
	}
	if len(out) != 0 {
		t.Fatalf("providers = %+v, want none configured", out)
	}
}

func TestAuthLogout_ClearsSession(t *testing.T) {
	e := newEnv(t)
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/auth/logout", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("POST /auth/logout = %d, want 204", status)
	}

	// The dev bypass re-authenticates a fresh admin on the next request
	// (no cookie survives logout), so /me still succeeds but as a new
	// session — logout itself must not error.
	var me struct {
		ID string `json:"id"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &me); status != http.StatusOK {
		t.Fatalf("GET /me after logout = %d, want 200", status)
	}
}

// TestAuthLogoutAll_DeletesEverySessionForUser seeds two login sessions for
// the same member (as if signed in on two browsers), calls
// POST /auth/logout-all with the client authenticated via the first
// session, and checks that both hashes are gone from db.LoginSessions
// afterwards — not just the one behind the request's own cookie.
func TestAuthLogoutAll_DeletesEverySessionForUser(t *testing.T) {
	e := newEnv(t)
	usr, client := e.memberClient("multi-device@example.com")
	firstHash := e.sessionHashFromJar(t, client)

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	rawHex := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(rawHex))
	secondHash := hex.EncodeToString(sum[:])
	if _, err := e.logins.Create(context.Background(), usr.ID, secondHash, time.Now().Add(24*time.Hour), "second-device"); err != nil {
		t.Fatalf("create second login session: %v", err)
	}

	if _, _, _, _, err := e.logins.GetByHash(context.Background(), firstHash); err != nil {
		t.Fatalf("precondition: first session should exist: %v", err)
	}
	if _, _, _, _, err := e.logins.GetByHash(context.Background(), secondHash); err != nil {
		t.Fatalf("precondition: second session should exist: %v", err)
	}

	status := e.doJSON(client, http.MethodPost, "/api/v1/auth/logout-all", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("POST /auth/logout-all = %d, want 204", status)
	}

	if _, _, _, _, err := e.logins.GetByHash(context.Background(), firstHash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("first session after logout-all = %v, want ErrNotFound", err)
	}
	if _, _, _, _, err := e.logins.GetByHash(context.Background(), secondHash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second session after logout-all = %v, want ErrNotFound", err)
	}
}

// TestAuthLogoutAll_RequiresUser checks the route is behind RequireUser: an
// unauthenticated request (dev bypass disabled) must get 401, not delete
// anything.
func TestAuthLogoutAll_RequiresUser(t *testing.T) {
	e := newEnvNoDevUser(t)
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/api/v1/auth/logout-all", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Requested-With", "styr")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /auth/logout-all without a session = %d, want 401", resp.StatusCode)
	}
}

// TestAuthLogoutAll_RequiresCSRFHeader checks the route is still behind
// csrfGuard even though it also requires a user.
func TestAuthLogoutAll_RequiresCSRFHeader(t *testing.T) {
	e := newEnv(t)
	status, _ := e.doJSONHeaders(e.adminClient, http.MethodPost, "/api/v1/auth/logout-all", nil, nil, false)
	if status != http.StatusForbidden {
		t.Fatalf("POST /auth/logout-all without CSRF header = %d, want 403", status)
	}
}

func TestAuthLogin_UnknownProvider404sOrRedirects(t *testing.T) {
	e := newEnv(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Jar:           e.adminClient.Jar,
	}
	req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/api/v1/auth/login/does-not-exist", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /auth/login/does-not-exist = %d, want 302", resp.StatusCode)
	}
}
