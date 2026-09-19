package api_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// codexKeyDTO mirrors the codex_key summary on GET /me and GET /settings: present and a
// label, never the key.
type codexKeyDTO struct {
	Present bool   `json:"present"`
	Label   string `json:"label"`
}

func TestMeCodexKey_AbsentByDefault(t *testing.T) {
	e := newEnv(t)
	var out struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &out); status != http.StatusOK {
		t.Fatalf("GET /me = %d, want 200", status)
	}
	if out.CodexKey.Present || out.CodexKey.Label != "" {
		t.Fatalf("codex_key = %+v, want absent", out.CodexKey)
	}
}

func TestMeCodexKey_PutVerifiesStoresAndLabels(t *testing.T) {
	e := newEnv(t)

	status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/me/codex-key", map[string]string{"key": "sk-proj-abcd1234wxyz"}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("PUT /me/codex-key = %d, want 204", status)
	}

	var out struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &out); status != http.StatusOK {
		t.Fatalf("GET /me = %d, want 200", status)
	}
	if !out.CodexKey.Present {
		t.Fatalf("codex_key = %+v, want present", out.CodexKey)
	}
	if out.CodexKey.Label != "…wxyz" {
		t.Fatalf("label = %q, want the last four characters", out.CodexKey.Label)
	}

	// What is stored is the sealed key, and only the ciphertext ever leaves the box.
	ciphertext, nonce, _, _, err := e.codexCreds.Get(context.Background(), e.adminID)
	if err != nil {
		t.Fatalf("read stored credential: %v", err)
	}
	if strings.Contains(string(ciphertext), "sk-proj-abcd1234wxyz") {
		t.Fatal("the key was stored in the clear")
	}
	plain, err := e.box.Open(ciphertext, nonce)
	if err != nil {
		t.Fatalf("open sealed key: %v", err)
	}
	if string(plain) != "sk-proj-abcd1234wxyz" {
		t.Fatalf("sealed key round-tripped as %q", plain)
	}
}

// A key the verifier rejects is never stored, and the response says why without quoting the
// key back.
func TestMeCodexKey_RejectedKeyStoresNothing(t *testing.T) {
	e := newEnv(t)
	e.codexVerifier.setErr(errors.New("codex: verify: key rejected: codex exited 1: 401 Unauthorized"))

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/me/codex-key", map[string]string{"key": "sk-bad"}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PUT /me/codex-key with a bad key = %d, want 422", status)
	}
	if body.Error.Code != "key_invalid" {
		t.Fatalf("error code = %q, want key_invalid", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, "401 Unauthorized") {
		t.Fatalf("message = %q, want the verifier's own detail", body.Error.Message)
	}
	if strings.Contains(body.Error.Message, "sk-bad") {
		t.Fatalf("message quoted the key back: %q", body.Error.Message)
	}

	var out struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &out)
	if out.CodexKey.Present {
		t.Fatal("a rejected key was stored")
	}
}

func TestMeCodexKey_MissingKeyIs422(t *testing.T) {
	e := newEnv(t)
	if status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/me/codex-key", map[string]string{}, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("PUT /me/codex-key with no key = %d, want 422", status)
	}
}

func TestMeCodexKey_DeleteIsIdempotent(t *testing.T) {
	e := newEnv(t)
	if status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/me/codex-key", map[string]string{"key": "sk-proj-abcd1234wxyz"}, nil); status != http.StatusNoContent {
		t.Fatalf("PUT /me/codex-key = %d, want 204", status)
	}
	for i := 0; i < 2; i++ {
		if status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/me/codex-key", nil, nil); status != http.StatusNoContent {
			t.Fatalf("DELETE /me/codex-key (call %d) = %d, want 204", i+1, status)
		}
	}
	var out struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &out)
	if out.CodexKey.Present {
		t.Fatal("the key survived the delete")
	}
}

// Each user's key is their own: a member's key never shows up on the admin's /me.
func TestMeCodexKey_IsPerUser(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member@example.com")
	if status := e.doJSON(member, http.MethodPut, "/api/v1/me/codex-key", map[string]string{"key": "sk-member-0001"}, nil); status != http.StatusNoContent {
		t.Fatalf("member PUT /me/codex-key = %d, want 204", status)
	}

	var adminMe struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &adminMe)
	if adminMe.CodexKey.Present {
		t.Fatal("the member's key leaked onto the admin's /me")
	}

	var memberMe struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	e.doJSON(member, http.MethodGet, "/api/v1/me", nil, &memberMe)
	if !memberMe.CodexKey.Present || memberMe.CodexKey.Label != "…0001" {
		t.Fatalf("member codex_key = %+v", memberMe.CodexKey)
	}
}

func TestSettingsCodexKey_PutAndSummary(t *testing.T) {
	e := newEnv(t)

	var before struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/settings", nil, &before); status != http.StatusOK {
		t.Fatalf("GET /settings = %d, want 200", status)
	}
	if before.CodexKey.Present {
		t.Fatalf("service codex_key = %+v, want absent", before.CodexKey)
	}

	if status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/settings/codex-key", map[string]string{"key": "sk-service-9999"}, nil); status != http.StatusNoContent {
		t.Fatalf("PUT /settings/codex-key = %d, want 204", status)
	}

	var after struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	e.doJSON(e.adminClient, http.MethodGet, "/api/v1/settings", nil, &after)
	if !after.CodexKey.Present || after.CodexKey.Label != "…9999" {
		t.Fatalf("service codex_key = %+v, want present with a last-four label", after.CodexKey)
	}

	// Stored under the service sentinel, not under the admin's own id.
	if _, _, _, _, err := e.codexCreds.GetService(context.Background()); err != nil {
		t.Fatalf("GetService: %v", err)
	}
	var me struct {
		CodexKey codexKeyDTO `json:"codex_key"`
	}
	e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &me)
	if me.CodexKey.Present {
		t.Fatal("the service key showed up as the admin's own")
	}
}

func TestSettingsCodexKey_MemberForbidden(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member@example.com")
	if status := e.doJSON(member, http.MethodPut, "/api/v1/settings/codex-key", map[string]string{"key": "sk-service-9999"}, nil); status != http.StatusForbidden {
		t.Fatalf("member PUT /settings/codex-key = %d, want 403", status)
	}
}
