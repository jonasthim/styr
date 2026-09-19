package api_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// runGitT runs a git command for test fixture setup, failing the test on
// error.
func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newBareRepoWithCommit creates a bare git repo (with one commit on "main")
// in a fresh temp directory and returns its path.
func newBareRepoWithCommit(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "repo.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare repo: %v", err)
	}
	runGitT(t, bare, "init", "--bare", "-b", "main")

	work := t.TempDir()
	runGitT(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, work, "add", "README.md")
	runGitT(t, work, "commit", "-m", "init")
	runGitT(t, work, "remote", "add", "origin", bare)
	runGitT(t, work, "push", "origin", "main")
	return bare
}

type workspaceOut struct {
	ID               string  `json:"id"`
	OwnerID          *string `json:"owner_id"`
	Name             string  `json:"name"`
	Path             string  `json:"path"`
	Source           string  `json:"source"`
	Managed          bool    `json:"managed"`
	State            string  `json:"state"`
	Error            string  `json:"error"`
	DefaultProfileID string  `json:"default_profile_id"`
	Worktrees        bool    `json:"worktrees"`
}

type errorOut struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// pollWorkspaceState polls GET /workspaces/{id} until its state matches
// want, failing the test after 10s.
func pollWorkspaceState(t *testing.T, e *testEnv, client *http.Client, id, want string) workspaceOut {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var out workspaceOut
		status := e.doJSON(client, http.MethodGet, "/api/v1/workspaces/"+id, nil, &out)
		if status != http.StatusOK {
			t.Fatalf("GET /workspaces/%s = %d", id, status)
		}
		if out.State == want {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace %s: state = %s (error=%q), want %s", id, out.State, out.Error, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWorkspacesCreate_Empty_MemberOwned(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member@example.com")

	var out workspaceOut
	status := e.doJSON(member, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "scratch", "source": "empty",
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /workspaces empty = %d, want 201", status)
	}
	if out.State != "ready" {
		t.Fatalf("state = %q, want ready", out.State)
	}
	if !out.Managed {
		t.Fatalf("managed = false, want true")
	}
	if out.DefaultProfileID != "interactive" {
		t.Fatalf("default_profile_id = %q, want interactive (the default)", out.DefaultProfileID)
	}
	if _, err := os.Stat(filepath.Join(out.Path, ".git")); err != nil {
		t.Fatalf("git init did not run: %v", err)
	}
}

func TestWorkspacesCreate_Git_ReachesReady(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member@example.com")
	bare := newBareRepoWithCommit(t)

	var out workspaceOut
	status := e.doJSON(member, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "cloned", "source": "git", "repo_url": "file://" + bare,
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /workspaces git = %d, want 201", status)
	}
	if out.State != "cloning" {
		t.Fatalf("initial state = %q, want cloning", out.State)
	}

	ready := pollWorkspaceState(t, e, member, out.ID, "ready")
	if _, err := os.Stat(filepath.Join(ready.Path, ".git")); err != nil {
		t.Fatalf("clone not present: %v", err)
	}
}

func TestWorkspacesCreate_Git_InvalidURL(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member@example.com")

	var body errorOut
	status := e.doJSON(member, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "bad", "source": "git", "repo_url": "not-a-url",
	}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /workspaces bad url = %d, want 422", status)
	}
	if body.Error.Code != "invalid_url" {
		t.Errorf("error code = %q, want invalid_url", body.Error.Code)
	}
}

func TestWorkspacesCreate_Path_RejectsNonGitDir(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir() // no .git inside

	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "bad", "source": "path", "path": dir,
	}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /workspaces with a non-git path = %d, want 422", status)
	}
	if body.Error.Code != "invalid_path" {
		t.Errorf("error code = %q, want invalid_path", body.Error.Code)
	}
}

func TestWorkspacesCreate_Path_AcceptsGitDirAndRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	_, memberClient := e.memberClient("member@example.com")

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	status := e.doJSON(memberClient, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "ws1", "source": "path", "path": dir,
	}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("POST /workspaces path as member = %d, want 403", status)
	}

	var out workspaceOut
	status = e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "ws1", "source": "path", "path": dir,
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /workspaces path as admin = %d, want 201", status)
	}
	if out.ID == "" {
		t.Fatalf("created workspace has no id")
	}
	if out.OwnerID != nil {
		t.Fatalf("owner_id = %v, want nil (shared)", out.OwnerID)
	}
	if out.Managed {
		t.Fatalf("managed = true, want false")
	}

	var list []workspaceOut
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/workspaces", nil, &list); status != http.StatusOK {
		t.Fatalf("GET /workspaces = %d, want 200", status)
	}
	if len(list) != 1 {
		t.Fatalf("workspaces = %+v, want exactly the one created", list)
	}
}

func TestWorkspacesCreate_DuplicateNameSameOwner_NameTaken(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member@example.com")

	status := e.doJSON(member, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "dup", "source": "empty"}, nil)
	if status != http.StatusCreated {
		t.Fatalf("first create = %d, want 201", status)
	}

	var body errorOut
	status = e.doJSON(member, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "dup", "source": "empty"}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate create = %d, want 422", status)
	}
	if body.Error.Code != "name_taken" {
		t.Errorf("error code = %q, want name_taken", body.Error.Code)
	}
}

func TestWorkspacesGet_NotVisibleToOtherMember(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("owner@example.com")
	_, other := e.memberClient("other@example.com")

	var out workspaceOut
	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "mine", "source": "empty"}, &out); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}

	var body errorOut
	status := e.doJSON(other, http.MethodGet, "/api/v1/workspaces/"+out.ID, nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET by other member = %d, want 404", status)
	}
}

func TestWorkspacesList_ExcludesOtherMembersPrivateWorkspaces(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("owner2@example.com")
	_, other := e.memberClient("other2@example.com")

	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "private", "source": "empty"}, nil); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}

	var list []workspaceOut
	if status := e.doJSON(other, http.MethodGet, "/api/v1/workspaces", nil, &list); status != http.StatusOK {
		t.Fatalf("GET /workspaces = %d", status)
	}
	if len(list) != 0 {
		t.Fatalf("other member's list = %+v, want empty", list)
	}
}

func TestWorkspacesPatch_OwnerCanUpdate(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("owner3@example.com")

	var out workspaceOut
	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "patchme", "source": "empty"}, &out); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}

	var patched workspaceOut
	status := e.doJSON(owner, http.MethodPatch, "/api/v1/workspaces/"+out.ID, map[string]any{"worktrees": true}, &patched)
	if status != http.StatusOK {
		t.Fatalf("PATCH = %d, want 200", status)
	}
	if !patched.Worktrees {
		t.Fatalf("worktrees = false, want true after patch")
	}
}

func TestWorkspacesPatch_ForbiddenForOtherMemberOnSharedWorkspace(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("member4@example.com")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	var out workspaceOut
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "shared4", "source": "path", "path": dir}, &out); status != http.StatusCreated {
		t.Fatalf("create shared: %d", status)
	}

	status := e.doJSON(member, http.MethodPatch, "/api/v1/workspaces/"+out.ID, map[string]any{"worktrees": true}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("PATCH shared workspace by member = %d, want 403", status)
	}
}

func TestWorkspacesDelete_RefusesWithOpenSession(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("owner5@example.com")

	var out workspaceOut
	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "busy", "source": "empty", "default_profile_id": "interactive"}, &out); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}

	ownerID := "owner5@example.com"
	sess := domain.Session{
		ID: "sess-busy", OwnerID: &ownerID, Title: "t", WorkspaceID: out.ID, ProfileID: "interactive",
		Harness: "claude", State: domain.SessionRunning, Origin: domain.OriginUI,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := e.sessions.Create(context.Background(), sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	var body errorOut
	status := e.doJSON(owner, http.MethodDelete, "/api/v1/workspaces/"+out.ID, nil, &body)
	if status != http.StatusConflict {
		t.Fatalf("DELETE with open session = %d, want 409", status)
	}
}

func TestWorkspacesDelete_RemovesManagedDir(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("owner6@example.com")

	var out workspaceOut
	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "gone", "source": "empty"}, &out); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}

	status := e.doJSON(owner, http.MethodDelete, "/api/v1/workspaces/"+out.ID, nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", status)
	}
	if _, err := os.Stat(out.Path); !os.IsNotExist(err) {
		t.Fatalf("expected managed dir removed, stat err = %v", err)
	}

	var body errorOut
	status = e.doJSON(owner, http.MethodGet, "/api/v1/workspaces/"+out.ID, nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET after delete = %d, want 404", status)
	}
}

func TestWorkspacesRetry_ReclonesAfterFailure(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("owner7@example.com")
	repoDir := filepath.Join(t.TempDir(), "later.git") // does not exist yet: first clone fails

	var out workspaceOut
	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "retry-me", "source": "git", "repo_url": "file://" + repoDir,
	}, &out); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}
	failed := pollWorkspaceState(t, e, owner, out.ID, "failed")
	if failed.Error == "" {
		t.Fatalf("expected a non-empty error after failed clone")
	}

	// Make the remote exist now, then retry.
	bare := repoDir
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	runGitT(t, bare, "init", "--bare", "-b", "main")
	work := t.TempDir()
	runGitT(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, work, "add", "README.md")
	runGitT(t, work, "commit", "-m", "init")
	runGitT(t, work, "remote", "add", "origin", bare)
	runGitT(t, work, "push", "origin", "main")

	var retried workspaceOut
	status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces/"+out.ID+"/retry", nil, &retried)
	if status != http.StatusOK {
		t.Fatalf("POST retry = %d, want 200", status)
	}
	if retried.State != "cloning" {
		t.Fatalf("state after retry = %q, want cloning", retried.State)
	}

	ready := pollWorkspaceState(t, e, owner, out.ID, "ready")
	if _, err := os.Stat(filepath.Join(ready.Path, ".git")); err != nil {
		t.Fatalf("clone not present after retry: %v", err)
	}
}

func TestSessionsCreate_RefusesCloningWorkspace(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("owner8@example.com")
	e.seedToken("owner8@example.com")
	missing := filepath.Join(t.TempDir(), "does-not-exist.git")

	var ws workspaceOut
	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "still-cloning", "source": "git", "repo_url": "file://" + missing,
	}, &ws); status != http.StatusCreated {
		t.Fatalf("create workspace: %d", status)
	}
	if ws.State != "cloning" {
		t.Fatalf("initial state = %q, want cloning", ws.State)
	}

	var body errorOut
	status := e.doJSON(owner, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "prompt": "hi",
	}, &body)
	if status != http.StatusConflict {
		t.Fatalf("POST /sessions against a cloning workspace = %d, want 409", status)
	}

	// Let the background clone finish (it will fail, but must not leak a
	// goroutine racing the test's temp-dir cleanup).
	pollWorkspaceState(t, e, owner, ws.ID, "failed")
}

// --- workspace access (T64) -------------------------------------------------

type workspaceAccessOut struct {
	Access string `json:"access"`
	Users  []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"users"`
}

func TestWorkspaceAccess_RequiresAdmin(t *testing.T) {
	e := newEnv(t)
	_, member := e.memberClient("access-member@example.com")

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	var shared workspaceOut
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces",
		map[string]any{"name": "access-admin-only-shared", "source": "path", "path": dir}, &shared); status != http.StatusCreated {
		t.Fatalf("create shared: %d", status)
	}

	if status := e.doJSON(member, http.MethodGet, "/api/v1/workspaces/"+shared.ID+"/access", nil, nil); status != http.StatusForbidden {
		t.Fatalf("GET access as member = %d, want 403", status)
	}
	if status := e.doJSON(member, http.MethodPut, "/api/v1/workspaces/"+shared.ID+"/access",
		map[string]any{"access": "listed", "user_ids": []string{}}, nil); status != http.StatusForbidden {
		t.Fatalf("PUT access as member = %d, want 403", status)
	}
}

func TestWorkspaceAccess_RejectsOwnedWorkspace(t *testing.T) {
	e := newEnv(t)
	_, owner := e.memberClient("access-owner@example.com")

	var out workspaceOut
	if status := e.doJSON(owner, http.MethodPost, "/api/v1/workspaces",
		map[string]any{"name": "owned-access", "source": "empty"}, &out); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}

	status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/workspaces/"+out.ID+"/access",
		map[string]any{"access": "listed", "user_ids": []string{}}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PUT access on an owned workspace = %d, want 422", status)
	}
}

func TestWorkspaceAccess_ListedRestrictsVisibilityAndSessionCreate(t *testing.T) {
	e := newEnv(t)
	allowed, allowedClient := e.memberClient("allowed@example.com")
	_, excludedClient := e.memberClient("excluded@example.com")

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	var ws workspaceOut
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces",
		map[string]any{"name": "listed-ws", "source": "path", "path": dir}, &ws); status != http.StatusCreated {
		t.Fatalf("create shared: %d", status)
	}

	// Defaults to "everyone": both members see it.
	var everyone []workspaceOut
	e.doJSON(excludedClient, http.MethodGet, "/api/v1/workspaces", nil, &everyone)
	found := false
	for _, w := range everyone {
		if w.ID == ws.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("workspace not visible before switching to listed access")
	}

	status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/workspaces/"+ws.ID+"/access",
		map[string]any{"access": "listed", "user_ids": []string{allowed.ID}}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("PUT access = %d, want 204", status)
	}

	var got workspaceAccessOut
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/workspaces/"+ws.ID+"/access", nil, &got); status != http.StatusOK {
		t.Fatalf("GET access = %d, want 200", status)
	}
	if got.Access != "listed" || len(got.Users) != 1 || got.Users[0].ID != allowed.ID {
		t.Fatalf("GET access = %+v, want listed with just %s", got, allowed.ID)
	}

	// The listed user still sees and can use it.
	var listedView []workspaceOut
	if status := e.doJSON(allowedClient, http.MethodGet, "/api/v1/workspaces", nil, &listedView); status != http.StatusOK {
		t.Fatalf("GET /workspaces (allowed) = %d", status)
	}
	found = false
	for _, w := range listedView {
		if w.ID == ws.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("listed user cannot see the workspace they were named on")
	}

	// The excluded member no longer sees it, in either the list or a direct
	// Get, and cannot start a session on it by hand.
	var excludedView []workspaceOut
	if status := e.doJSON(excludedClient, http.MethodGet, "/api/v1/workspaces", nil, &excludedView); status != http.StatusOK {
		t.Fatalf("GET /workspaces (excluded) = %d", status)
	}
	for _, w := range excludedView {
		if w.ID == ws.ID {
			t.Fatalf("excluded member still sees the listed workspace")
		}
	}
	if status := e.doJSON(excludedClient, http.MethodGet, "/api/v1/workspaces/"+ws.ID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("GET workspace directly (excluded) = %d, want 404", status)
	}

	var sessBody errorOut
	status = e.doJSON(excludedClient, http.MethodPost, "/api/v1/sessions",
		map[string]any{"workspace_id": ws.ID, "profile_id": "interactive", "prompt": "hi"}, &sessBody)
	if status != http.StatusNotFound {
		t.Fatalf("POST /sessions against a listed workspace (excluded member) = %d, want 404", status)
	}

	// An admin can always see and use it regardless of the list.
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/workspaces/"+ws.ID, nil, nil); status != http.StatusOK {
		t.Fatalf("GET workspace directly (admin) = %d, want 200", status)
	}

	// Switching back to "everyone" restores visibility for the excluded
	// member.
	status = e.doJSON(e.adminClient, http.MethodPut, "/api/v1/workspaces/"+ws.ID+"/access",
		map[string]any{"access": "everyone", "user_ids": []string{}}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("PUT access back to everyone = %d, want 204", status)
	}
	if status := e.doJSON(excludedClient, http.MethodGet, "/api/v1/workspaces/"+ws.ID, nil, nil); status != http.StatusOK {
		t.Fatalf("GET workspace directly after reverting to everyone = %d, want 200", status)
	}
}
