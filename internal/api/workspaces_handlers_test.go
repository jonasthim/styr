package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacesCreate_RejectsNonGitPath(t *testing.T) {
	e := newEnv(t)

	dir := t.TempDir() // no .git inside
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "bad", "path": dir, "default_profile_id": "interactive",
	}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /workspaces with a non-git path = %d, want 422", status)
	}
	if body.Error.Code != "invalid_path" {
		t.Errorf("error code = %q, want invalid_path", body.Error.Code)
	}
}

func TestWorkspacesCreate_AcceptsGitDirAndRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	_, memberClient := e.memberClient("member@example.com")

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	status := e.doJSON(memberClient, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "ws1", "path": dir, "default_profile_id": "interactive",
	}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("POST /workspaces as member = %d, want 403", status)
	}

	var out struct {
		ID string `json:"id"`
	}
	status = e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "ws1", "path": dir, "default_profile_id": "interactive",
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /workspaces as admin = %d, want 201", status)
	}
	if out.ID == "" {
		t.Fatalf("created workspace has no id")
	}

	var list []map[string]any
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/workspaces", nil, &list); status != http.StatusOK {
		t.Fatalf("GET /workspaces = %d, want 200", status)
	}
	if len(list) != 1 {
		t.Fatalf("workspaces = %+v, want exactly the one created", list)
	}
}
