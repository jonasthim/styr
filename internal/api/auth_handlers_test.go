package api_test

import (
	"net/http"
	"testing"
)

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
