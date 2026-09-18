package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jonasthim/styr/internal/notify"
)

type notificationOut struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	TokenPresent bool     `json:"token_present"`
	Token        *string  `json:"token"` // must never be populated by the server
	Events       []string `json:"events"`
	Enabled      bool     `json:"enabled"`
}

func TestNotificationsCreate_SealsTokenAndNeverReturnsIt(t *testing.T) {
	e := newEnv(t)

	var out notificationOut
	_, raw := e.doJSONHeaders(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "ntfy", "name": "on-call", "url": "https://ntfy.sh/styr-alerts", "token": "super-secret-token",
	}, &out, true)
	if out.ID == "" {
		t.Fatalf("out = %+v, want a generated id", out)
	}
	if !out.TokenPresent {
		t.Fatal("token_present = false, want true after creating with a token")
	}
	if out.Token != nil {
		t.Fatalf("token field populated: %+v", out)
	}
	if containsSubstring(string(raw), "super-secret-token") {
		t.Fatalf("response leaked the plaintext token: %s", raw)
	}

	// The stored row holds a sealed token, never the plaintext.
	stored, err := e.notifications.Get(context.Background(), out.ID)
	if err != nil {
		t.Fatalf("get stored channel: %v", err)
	}
	if len(stored.TokenCiphertext) == 0 || len(stored.TokenNonce) == 0 {
		t.Fatalf("stored channel has no sealed token: %+v", stored)
	}
	plain, err := e.box.Open(stored.TokenCiphertext, stored.TokenNonce)
	if err != nil {
		t.Fatalf("open stored token: %v", err)
	}
	if string(plain) != "super-secret-token" {
		t.Fatalf("decrypted stored token = %q, want the original plaintext", plain)
	}
}

func TestNotificationsCreate_RejectsBadKind(t *testing.T) {
	e := newEnv(t)
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "carrier-pigeon", "name": "x", "url": "https://example.com",
	}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
}

func TestNotificationsCreate_RequiresAdmin(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member-notify@example.com")
	status := e.doJSON(member, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "webhook", "name": "x", "url": "https://example.com",
	}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("member create = %d, want 403", status)
	}
}

func TestNotificationsList_NeverIncludesToken(t *testing.T) {
	e := newEnv(t)
	var created notificationOut
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "webhook", "name": "wh", "url": "https://example.com/hook", "token": "abc123",
	}, &created); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}

	var list []notificationOut
	_, raw := e.doJSONHeaders(e.adminClient, http.MethodGet, "/api/v1/notifications", nil, &list, false)
	if len(list) != 1 || !list[0].TokenPresent {
		t.Fatalf("list = %+v", list)
	}
	if containsSubstring(string(raw), "abc123") {
		t.Fatalf("list response leaked the token: %s", raw)
	}
}

func TestNotificationsPatch_EmptyTokenClearsIt(t *testing.T) {
	e := newEnv(t)
	var created notificationOut
	e.doJSON(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "ntfy", "name": "n", "url": "https://ntfy.sh/x", "token": "will-be-cleared",
	}, &created)

	var patched notificationOut
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/notifications/"+created.ID,
		map[string]any{"token": ""}, &patched)
	if status != http.StatusOK {
		t.Fatalf("PATCH = %d, want 200", status)
	}
	if patched.TokenPresent {
		t.Fatal("token_present = true after clearing with an empty token")
	}
}

func TestNotificationsPatch_OmittedTokenLeavesItUnchanged(t *testing.T) {
	e := newEnv(t)
	var created notificationOut
	e.doJSON(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "ntfy", "name": "n", "url": "https://ntfy.sh/x", "token": "keep-me",
	}, &created)

	var patched notificationOut
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/notifications/"+created.ID,
		map[string]any{"name": "renamed"}, &patched)
	if status != http.StatusOK {
		t.Fatalf("PATCH = %d, want 200", status)
	}
	if !patched.TokenPresent {
		t.Fatal("token_present = false after a patch that did not mention token")
	}
	if patched.Name != "renamed" {
		t.Fatalf("name = %q, want renamed", patched.Name)
	}
}

func TestNotificationsDelete_OK(t *testing.T) {
	e := newEnv(t)
	var created notificationOut
	e.doJSON(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "webhook", "name": "n", "url": "https://example.com",
	}, &created)

	status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/notifications/"+created.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", status)
	}
}

func TestNotificationsTest_CallsNotifierWithDecryptedToken(t *testing.T) {
	e := newEnv(t)
	var created notificationOut
	e.doJSON(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "ntfy", "name": "on-call", "url": "https://ntfy.sh/styr-alerts", "token": "decrypt-me-please",
	}, &created)

	var out map[string]bool
	_, raw := e.doJSONHeaders(e.adminClient, http.MethodPost, "/api/v1/notifications/"+created.ID+"/test", nil, &out, true)
	if !out["ok"] {
		t.Fatalf("test response = %+v, want ok=true", out)
	}
	if containsSubstring(string(raw), "decrypt-me-please") {
		t.Fatalf("test response leaked the token: %s", raw)
	}

	call, ok := e.notifier.lastCall()
	if !ok {
		t.Fatal("Notifier.Send was not called")
	}
	if call.channel.Token != "decrypt-me-please" {
		t.Fatalf("channel token passed to Notifier = %q, want the decrypted plaintext", call.channel.Token)
	}
	if call.event.Kind != "test" {
		t.Fatalf("event kind = %q, want test", call.event.Kind)
	}
}

func TestNotificationsTest_SendFailureReturns502NoDetail(t *testing.T) {
	e := newEnv(t)
	var created notificationOut
	e.doJSON(e.adminClient, http.MethodPost, "/api/v1/notifications", map[string]any{
		"kind": "webhook", "name": "n", "url": "https://example.com",
	}, &created)

	e.notifier.SendFn = func(context.Context, notify.Channel, notify.Event) error {
		return context.DeadlineExceeded
	}
	var body errorOut
	status, raw := e.doJSONHeaders(e.adminClient, http.MethodPost, "/api/v1/notifications/"+created.ID+"/test", nil, &body, true)
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", status)
	}
	if body.Error.Code != "notify_failed" {
		t.Fatalf("error code = %q, want notify_failed", body.Error.Code)
	}
	if containsSubstring(string(raw), "deadline") || containsSubstring(string(raw), "context") {
		t.Fatalf("error body leaked internal detail: %s", raw)
	}
}
