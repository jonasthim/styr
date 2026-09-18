package api_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestMeGet_Unauthenticated401(t *testing.T) {
	// The dev-user bypass (used by every other test's admin client) would
	// otherwise auto-provision an admin for any cookie-less request, so a
	// true "not logged in" 401 needs the bypass disabled.
	e := newEnvNoDevUser(t)
	status, _ := e.doJSONHeaders(&http.Client{}, http.MethodGet, "/api/v1/sessions", nil, nil, true)
	if status != http.StatusUnauthorized {
		t.Fatalf("GET /sessions with no cookie = %d, want 401", status)
	}
}

func TestMeGet_ReturnsAdmin(t *testing.T) {
	e := newEnv(t)
	var me struct {
		Role        string `json:"role"`
		ClaudeToken struct {
			Present bool `json:"present"`
		} `json:"claude_token"`
	}
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &me)
	if status != http.StatusOK {
		t.Fatalf("GET /me = %d, want 200", status)
	}
	if me.Role != "admin" {
		t.Errorf("role = %q, want admin", me.Role)
	}
	if me.ClaudeToken.Present {
		t.Errorf("claude_token.present = true before any token is set")
	}
}

func TestMeTokenPut_NeverEchoesToken(t *testing.T) {
	e := newEnv(t)
	const token = "sk-ant-oat01-secretvalue"

	status, raw := e.doJSONHeaders(e.adminClient, http.MethodPut, "/api/v1/me/claude-token",
		map[string]string{"token": token}, nil, true)
	if status != http.StatusNoContent {
		t.Fatalf("PUT /me/claude-token = %d, want 204", status)
	}
	if strings.Contains(string(raw), token) {
		t.Fatalf("response body contains the raw token: %q", raw)
	}

	var me struct {
		ClaudeToken struct {
			Present    bool   `json:"present"`
			Label      string `json:"label"`
			VerifiedAt string `json:"verified_at"`
		} `json:"claude_token"`
	}
	status, raw = e.doJSONHeaders(e.adminClient, http.MethodGet, "/api/v1/me", nil, &me, true)
	if status != http.StatusOK {
		t.Fatalf("GET /me = %d, want 200", status)
	}
	if strings.Contains(string(raw), token) {
		t.Fatalf("GET /me body contains the raw token: %q", raw)
	}
	if !me.ClaudeToken.Present {
		t.Fatalf("claude_token.present = false after PUT")
	}
	if !strings.HasSuffix(me.ClaudeToken.Label, "value") {
		t.Errorf("label = %q, want a 6-char suffix label", me.ClaudeToken.Label)
	}
	if me.ClaudeToken.VerifiedAt == "" {
		t.Errorf("verified_at is empty after a successful verify")
	}
}

func TestMeTokenPut_VerifyFailureStoresNothing(t *testing.T) {
	e := newEnv(t)
	e.verifier.setErr(errors.New("token rejected"))

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/me/claude-token", map[string]string{"token": "sk-bad"}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PUT /me/claude-token with a bad token = %d, want 422", status)
	}
	if body.Error.Code != "token_invalid" {
		t.Errorf("error code = %q, want token_invalid", body.Error.Code)
	}

	var me struct {
		ClaudeToken struct {
			Present bool `json:"present"`
		} `json:"claude_token"`
	}
	e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &me)
	if me.ClaudeToken.Present {
		t.Fatalf("claude_token.present = true after a failed verify; nothing should have been stored")
	}
}

func TestMeTokenDelete_IsIdempotent(t *testing.T) {
	e := newEnv(t)
	status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/me/claude-token", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /me/claude-token with none set = %d, want 204", status)
	}
}

func TestPost_WithoutCSRFHeader403(t *testing.T) {
	e := newEnv(t)
	status, _ := e.doJSONHeaders(e.adminClient, http.MethodPatch, "/api/v1/me", map[string]any{"prefs": map[string]any{"a": 1}}, nil, false)
	if status != http.StatusForbidden {
		t.Fatalf("PATCH /me without X-Requested-With = %d, want 403", status)
	}
}
