package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/auth"
)

// bearerClient wraps client requests with an Authorization: Bearer header
// instead of a cookie jar, so doJSONHeaders exercises the token-auth path.
// http.Client has no per-request header hook, so this wraps RoundTrip.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (t bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func bearerClient(token string) *http.Client {
	return &http.Client{Transport: bearerRoundTripper{token: token}}
}

func TestAPITokens_CreateThenList_ShowsPrefixNotToken(t *testing.T) {
	e := newEnv(t)

	var created struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Prefix string `json:"prefix"`
		Token  string `json:"token"`
	}
	status, raw := e.doJSONHeaders(e.adminClient, http.MethodPost, "/api/v1/me/api-tokens",
		map[string]any{"name": "ci"}, &created, true)
	if status != http.StatusCreated {
		t.Fatalf("POST /me/api-tokens = %d, want 201 (body %s)", status, raw)
	}
	if created.Token == "" || !strings.HasPrefix(created.Token, auth.APITokenPrefix) {
		t.Fatalf("created token = %q, want a styr_pat_ prefixed secret", created.Token)
	}
	if created.Prefix == "" || created.Prefix == created.Token {
		t.Fatalf("created prefix = %q, want a short prefix distinct from the full token", created.Prefix)
	}

	var list []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Prefix string `json:"prefix"`
	}
	status, raw = e.doJSONHeaders(e.adminClient, http.MethodGet, "/api/v1/me/api-tokens", nil, &list, true)
	if status != http.StatusOK {
		t.Fatalf("GET /me/api-tokens = %d, want 200", status)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v, want exactly one token", list)
	}
	if list[0].ID != created.ID || list[0].Prefix != created.Prefix {
		t.Errorf("listed token = %+v, want id/prefix matching the created one %+v", list[0], created)
	}
	if strings.Contains(string(raw), created.Token) {
		t.Fatalf("GET /me/api-tokens response contains the raw token: %q", raw)
	}
}

func TestAPITokens_BearerAuth_ReachesMe_NoCookieNoCSRFHeader(t *testing.T) {
	e := newEnv(t)

	var created struct {
		Token string `json:"token"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/me/api-tokens", map[string]any{"name": "ci"}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /me/api-tokens = %d, want 201", status)
	}

	client := bearerClient(created.Token)

	// GET /me with only the bearer header, no cookie at all.
	var me struct {
		Role string `json:"role"`
	}
	status = e.doJSON(client, http.MethodGet, "/api/v1/me", nil, &me)
	if status != http.StatusOK {
		t.Fatalf("GET /me with bearer auth = %d, want 200", status)
	}
	if me.Role != "admin" {
		t.Errorf("role = %q, want admin", me.Role)
	}

	// A state-changing request (PATCH /me) with the bearer header but
	// deliberately WITHOUT the X-Requested-With CSRF header must still
	// succeed: csrfGuard skips the check for a token-authenticated
	// principal (internal/api/middleware.go).
	status, _ = e.doJSONHeaders(client, http.MethodPatch, "/api/v1/me",
		map[string]any{"prefs": map[string]any{"a": 1}}, nil, false)
	if status != http.StatusOK {
		t.Fatalf("PATCH /me via bearer auth without CSRF header = %d, want 200", status)
	}
}

func TestAPITokens_Expired401(t *testing.T) {
	// The dev-user bypass (newEnv's default) would otherwise auto-provision
	// an admin session for any bearer-less/cookie-less request, masking a
	// genuinely-rejected expired token behind a 200 - see
	// TestMeGet_Unauthenticated401's comment on newEnvNoDevUser for the same
	// reasoning. A real login session (memberClient) creates and deletes
	// the token instead of the dev-bypass admin client.
	e := newEnvNoDevUser(t)
	_, client := e.memberClient("member@example.com")

	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	status := e.doJSON(client, http.MethodPost, "/api/v1/me/api-tokens",
		map[string]any{"name": "ci", "expires_in_days": 1}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /me/api-tokens = %d, want 201", status)
	}

	// Back-date the token's expiry directly in the fake store: the create
	// endpoint only accepts a future expires_in_days, so an already-expired
	// token has to be seeded this way.
	e.expireToken(created.ID)

	status = e.doJSON(bearerClient(created.Token), http.MethodGet, "/api/v1/sessions", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("GET /sessions with an expired bearer token = %d, want 401", status)
	}
}

func TestAPITokens_Deleted401(t *testing.T) {
	// See TestAPITokens_Expired401: the dev-user bypass must be disabled for
	// this assertion to mean anything.
	e := newEnvNoDevUser(t)
	_, client := e.memberClient("member@example.com")

	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	status := e.doJSON(client, http.MethodPost, "/api/v1/me/api-tokens", map[string]any{"name": "ci"}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /me/api-tokens = %d, want 201", status)
	}

	status = e.doJSON(client, http.MethodDelete, "/api/v1/me/api-tokens/"+created.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /me/api-tokens/{id} = %d, want 204", status)
	}

	status = e.doJSON(bearerClient(created.Token), http.MethodGet, "/api/v1/sessions", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("GET /sessions with a revoked bearer token = %d, want 401", status)
	}
}

func TestAPITokens_DeleteAnotherUsersToken404(t *testing.T) {
	e := newEnv(t)
	_, memberClient := e.memberClient("member@example.com")

	var created struct {
		ID string `json:"id"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/me/api-tokens", map[string]any{"name": "admin-token"}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /me/api-tokens (admin) = %d, want 201", status)
	}

	status = e.doJSON(memberClient, http.MethodDelete, "/api/v1/me/api-tokens/"+created.ID, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("member DELETE of admin's token = %d, want 404", status)
	}
}

// expireToken back-dates a token's expiry directly in the test fake store,
// bypassing the create endpoint (which rejects a non-positive
// expires_in_days).
func (e *testEnv) expireToken(id string) {
	e.t.Helper()
	e.tokenStore.mu.Lock()
	defer e.tokenStore.mu.Unlock()
	tok, ok := e.tokenStore.byID[id]
	if !ok {
		e.t.Fatalf("expireToken: no such token %q", id)
	}
	past := time.Now().Add(-time.Hour)
	tok.ExpiresAt = &past
	e.tokenStore.byID[id] = tok
}
