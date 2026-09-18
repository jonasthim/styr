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

// POST /sessions accepts a model and effort, and GET /sessions/{id} reports the effort and
// the (initially empty) slash-command list.
func TestSessionsCreate_ModelAndEffortRoundTrip(t *testing.T) {
	e := newEnv(t, createResultStep())
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("model-owner@example.com")
	e.seedToken(owner.ID)

	var created struct {
		ID            string   `json:"id"`
		Effort        string   `json:"effort"`
		SlashCommands []string `json:"slash_commands"`
	}
	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
		"model": "opus", "effort": "high",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}
	if created.Effort != "high" {
		t.Errorf("effort = %q, want high", created.Effort)
	}
	if created.SlashCommands == nil {
		t.Error("slash_commands is null, want an empty array")
	}

	var got struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if status := e.doJSON(ownerClient, http.MethodGet, "/api/v1/sessions/"+created.ID, nil, &got); status != http.StatusOK {
		t.Fatalf("GET /sessions/%s = %d, want 200", created.ID, status)
	}
	if got.Model != "opus" || got.Effort != "high" {
		t.Errorf("model/effort = %q/%q, want opus/high", got.Model, got.Effort)
	}
}

func TestSessionsCreate_UnknownEffortIs422(t *testing.T) {
	e := newEnv(t, createResultStep())
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("bad-effort@example.com")
	e.seedToken(owner.ID)

	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
		"effort": "turbo",
	}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /sessions with effort=turbo = %d, want 422", status)
	}
}

func TestSessionsModel_SwitchAccepted(t *testing.T) {
	e := newEnv(t, createResultStep())
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("switcher@example.com")
	e.seedToken(owner.ID)

	var created struct {
		ID string `json:"id"`
	}
	if status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
	}, &created); status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}

	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions/"+created.ID+"/model",
		map[string]any{"model": "haiku", "effort": "low"}, nil)
	if status != http.StatusAccepted {
		t.Fatalf("POST /sessions/{id}/model = %d, want 202", status)
	}

	var got struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if status := e.doJSON(ownerClient, http.MethodGet, "/api/v1/sessions/"+created.ID, nil, &got); status != http.StatusOK {
		t.Fatalf("GET /sessions/{id} = %d, want 200", status)
	}
	if got.Model != "haiku" || got.Effort != "low" {
		t.Errorf("model/effort after switch = %q/%q, want haiku/low", got.Model, got.Effort)
	}
}

func TestSessionsModel_UnknownEffortIs422(t *testing.T) {
	e := newEnv(t, createResultStep())
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("bad-switch@example.com")
	e.seedToken(owner.ID)

	var created struct {
		ID string `json:"id"`
	}
	if status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
	}, &created); status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}
	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions/"+created.ID+"/model",
		map[string]any{"model": "haiku", "effort": "turbo"}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /sessions/{id}/model with effort=turbo = %d, want 422", status)
	}
}

// POST /sessions with worktree_path set to another session's worktree reuses it instead of
// creating a fresh one, and the response reports it shared.
func TestSessionsCreate_WorktreePathReusesExistingWorktree(t *testing.T) {
	e := newEnv(t, createResultStep(), createResultStep())
	ws := e.seedGitWorkspace()
	e.seedToken(e.adminID)

	var first struct {
		ID       string `json:"id"`
		Worktree string `json:"worktree"`
		Branch   string `json:"branch"`
		BaseRef  string `json:"base_ref"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "step one", "prompt": "hello",
	}, &first)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions (first) = %d, want 201", status)
	}
	waitForSessionState(t, e, first.ID, "open")

	var second struct {
		ID             string `json:"id"`
		Worktree       string `json:"worktree"`
		Branch         string `json:"branch"`
		BaseRef        string `json:"base_ref"`
		WorktreeShared bool   `json:"worktree_shared"`
	}
	status = e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "step two", "prompt": "hello",
		"worktree_path": first.Worktree,
	}, &second)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions (worktree_path) = %d, want 201", status)
	}
	waitForSessionState(t, e, second.ID, "open")

	if second.Worktree != first.Worktree {
		t.Errorf("worktree = %q, want the first session's %q", second.Worktree, first.Worktree)
	}
	if second.Branch != first.Branch {
		t.Errorf("branch = %q, want the first session's %q", second.Branch, first.Branch)
	}
	if second.BaseRef != first.BaseRef {
		t.Errorf("base_ref = %q, want the first session's %q", second.BaseRef, first.BaseRef)
	}
	if !second.WorktreeShared {
		t.Error("worktree_shared = false, want true")
	}
}

// POST /sessions with a worktree_path that is not a registered worktree under the workspace's
// worktrees directory is refused with 422.
func TestSessionsCreate_WorktreePathInvalidIs422(t *testing.T) {
	e := newEnv(t, createResultStep())
	ws := e.seedGitWorkspace()
	e.seedToken(e.adminID)

	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
		"worktree_path": t.TempDir(),
	}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /sessions with a bogus worktree_path = %d, want 422", status)
	}
}

func TestSessionsModel_OtherUsersSessionIs404(t *testing.T) {
	e := newEnv(t, createResultStep())
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("mine@example.com")
	e.seedToken(owner.ID)
	_, otherClient := e.memberClient("theirs@example.com")

	var created struct {
		ID string `json:"id"`
	}
	if status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "hello",
	}, &created); status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}
	status := e.doJSON(otherClient, http.MethodPost, "/api/v1/sessions/"+created.ID+"/model",
		map[string]any{"model": "haiku"}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("POST /sessions/{id}/model on another member's session = %d, want 404", status)
	}
}
