package api_test

// Router-level coverage for the T64 writer guard (internal/api/middleware.go's
// writerGuard, backed by auth.RequireWriter): a viewer gets 403 code
// "read_only" on state-changing routes across several resources, but keeps
// full read access and full write access to their own /me routes; member
// and admin are unaffected.

import (
	"net/http"
	"testing"
)

func TestWriterGuard_ViewerRefusedOnStateChangingRoutes(t *testing.T) {
	e := newEnv(t)
	_, viewer := e.viewerClient("viewer1@example.com")

	dir := t.TempDir()
	// A workspace to aim the refused POST /sessions at; the request must be
	// refused by the writer guard before it ever reaches the sessions
	// service, so the workspace need not even be usable.
	var ws workspaceOut
	e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "viewer-ws", "source": "empty"}, &ws)
	_ = dir

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"create workspace", http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "nope", "source": "empty"}},
		{"create session", http.MethodPost, "/api/v1/sessions", map[string]any{"workspace_id": ws.ID, "profile_id": "interactive", "prompt": "hi"}},
		{"decide approval", http.MethodPost, "/api/v1/approvals/nonexistent", map[string]any{"decision": "allow"}},
		{"delete workspace", http.MethodDelete, "/api/v1/workspaces/" + ws.ID, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body errorOut
			status := e.doJSON(viewer, c.method, c.path, c.body, &body)
			if status != http.StatusForbidden {
				t.Fatalf("%s %s as viewer = %d, want 403", c.method, c.path, status)
			}
			if body.Error.Code != "read_only" {
				t.Errorf("%s %s error code = %q, want %q", c.method, c.path, body.Error.Code, "read_only")
			}
		})
	}
}

func TestWriterGuard_ViewerCanStillWriteOwnMeRoutes(t *testing.T) {
	e := newEnv(t)
	_, viewer := e.viewerClient("viewer2@example.com")

	// PATCH /me (prefs) is exempt.
	if status := e.doJSON(viewer, http.MethodPatch, "/api/v1/me", map[string]any{"prefs": map[string]any{"theme": "light"}}, nil); status != http.StatusOK {
		t.Fatalf("PATCH /me as viewer = %d, want 200", status)
	}

	// The viewer's own Claude token routes are exempt too.
	status := e.doJSON(viewer, http.MethodPut, "/api/v1/me/claude-token", map[string]any{"token": "not-a-real-token"}, nil)
	if status == http.StatusForbidden {
		t.Fatalf("PUT /me/claude-token as viewer = 403, want it to reach the handler (not the writer guard)")
	}
}

func TestWriterGuard_ViewerCanRead(t *testing.T) {
	e := newEnv(t)
	_, viewer := e.viewerClient("viewer3@example.com")

	if status := e.doJSON(viewer, http.MethodGet, "/api/v1/workspaces", nil, nil); status != http.StatusOK {
		t.Fatalf("GET /workspaces as viewer = %d, want 200", status)
	}
	if status := e.doJSON(viewer, http.MethodGet, "/api/v1/sessions", nil, nil); status != http.StatusOK {
		t.Fatalf("GET /sessions as viewer = %d, want 200", status)
	}
	if status := e.doJSON(viewer, http.MethodGet, "/api/v1/approvals", nil, nil); status != http.StatusOK {
		t.Fatalf("GET /approvals as viewer = %d, want 200", status)
	}
}

func TestWriterGuard_MemberAndAdminUnaffected(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("writer-member@example.com")

	for name, client := range map[string]*http.Client{"member": member, "admin": e.adminClient} {
		t.Run(name, func(t *testing.T) {
			var out workspaceOut
			status := e.doJSON(client, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": name + "-ws", "source": "empty"}, &out)
			if status != http.StatusCreated {
				t.Fatalf("POST /workspaces as %s = %d, want 201", name, status)
			}
		})
	}
}
