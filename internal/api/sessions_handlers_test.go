package api_test

import (
	"net/http"
	"testing"
)

func TestSessionsCreate_WithoutTokenIs422(t *testing.T) {
	e := newEnv(t)
	ws := e.seedWorkspace("interactive")
	_, member := e.memberClient("no-token@example.com")

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	status := e.doJSON(member, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
	}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /sessions without a token = %d, want 422", status)
	}
	if body.Error.Message == "" {
		t.Errorf("error message is empty, want a reason")
	}
}

func TestSessionsGet_VisibilityByOwner(t *testing.T) {
	e := newEnv(t)
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("owner@example.com")
	e.seedToken(owner.ID)
	_, otherClient := e.memberClient("other@example.com")

	var created struct {
		ID string `json:"id"`
	}
	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "owner's session", "prompt": "hello",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions as owner = %d, want 201", status)
	}

	if status := e.doJSON(otherClient, http.MethodGet, "/api/v1/sessions/"+created.ID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("GET /sessions/%s as another member = %d, want 404", created.ID, status)
	}
	if status := e.doJSON(ownerClient, http.MethodGet, "/api/v1/sessions/"+created.ID, nil, nil); status != http.StatusOK {
		t.Fatalf("GET /sessions/%s as owner = %d, want 200", created.ID, status)
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+created.ID, nil, nil); status != http.StatusOK {
		t.Fatalf("GET /sessions/%s as admin = %d, want 200", created.ID, status)
	}
}

func TestSessionsClose_NoRunningProcessStillCloses(t *testing.T) {
	e := newEnv(t)
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("closer@example.com")
	e.seedToken(owner.ID)

	var created struct {
		ID string `json:"id"`
	}
	e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
	}, &created)

	if status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions/"+created.ID+"/close", nil, nil); status != http.StatusNoContent {
		t.Fatalf("POST /sessions/%s/close = %d, want 204", created.ID, status)
	}
}
