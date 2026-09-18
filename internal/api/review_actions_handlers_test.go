package api_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

func TestReviewCommit(t *testing.T) {
	e, id, worktree := newReviewEnv(t)
	if err := os.WriteFile(filepath.Join(worktree, "feature.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	var out struct {
		SHA string `json:"sha"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/commit",
		map[string]any{"message": "feat: add feature"}, &out)
	if status != http.StatusOK {
		t.Fatalf("POST /commit = %d, want 200", status)
	}
	if out.SHA == "" {
		t.Fatal("sha is empty")
	}

	// Empty message: 422. Nothing left to commit: 409.
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/commit",
		map[string]any{"message": ""}, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /commit with an empty message = %d, want 422", status)
	}
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/commit",
		map[string]any{"message": "again"}, nil); status != http.StatusConflict {
		t.Fatalf("POST /commit with nothing to commit = %d, want 409", status)
	}
}

func TestReviewPR_NoRemoteIs409(t *testing.T) {
	e, id, _ := newReviewEnv(t)

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/pr",
		map[string]any{"title": "A title", "body": "A body"}, &body)
	if status != http.StatusConflict {
		t.Fatalf("POST /pr without a remote = %d, want 409", status)
	}
	if body.Error.Code != "no_remote" {
		t.Fatalf("error code = %q, want no_remote", body.Error.Code)
	}

	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/pr",
		map[string]any{"title": ""}, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /pr without a title = %d, want 422", status)
	}
}

func TestReviewCheckpointsAndRewind(t *testing.T) {
	e, id, worktree := newReviewEnv(t)

	// A turn that changed a file leaves a checkpoint behind.
	if err := os.WriteFile(filepath.Join(worktree, "file.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/messages",
		map[string]any{"text": "next"}, nil); status != http.StatusAccepted {
		t.Fatalf("POST /messages = %d, want 202", status)
	}

	var list []struct {
		ID        string `json:"id"`
		CommitSHA string `json:"commit_sha"`
		Turn      int    `json:"turn"`
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/checkpoints", nil, &list); status != http.StatusOK {
			t.Fatalf("GET /checkpoints = %d, want 200", status)
		}
		if len(list) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no checkpoint appeared after a turn that changed a file")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if list[0].CommitSHA == "" {
		t.Fatalf("checkpoint = %+v", list[0])
	}

	// Waiting for the diff stats keeps the rewind from racing the pump's
	// last write for that turn.
	waitForSessionDiff(t, e, id, 1)

	if err := os.WriteFile(filepath.Join(worktree, "file.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/rewind",
		map[string]any{"checkpoint_id": list[0].ID}, nil); status != http.StatusAccepted {
		t.Fatalf("POST /rewind = %d, want 202", status)
	}
	got, err := os.ReadFile(filepath.Join(worktree, "file.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "v1\n" {
		t.Fatalf("file.txt = %q, want the checkpointed v1", got)
	}

	// No checkpoint_id: 422. Running: 409.
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/rewind",
		map[string]any{}, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /rewind without a checkpoint_id = %d, want 422", status)
	}
	if err := e.sessions.UpdateState(context.Background(), id, domain.SessionRunning); err != nil {
		t.Fatalf("set running: %v", err)
	}
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/rewind",
		map[string]any{"checkpoint_id": list[0].ID}, nil); status != http.StatusConflict {
		t.Fatalf("POST /rewind while running = %d, want 409", status)
	}
}

// waitForSessionDiff polls GET /sessions/{id} until its diff_add counter
// reaches want.
// waitForSessionDiff polls GET /sessions/{id} until its diff_add counter
// reaches want.
func waitForSessionDiff(t *testing.T, e *testEnv, id string, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var sess struct {
			DiffAdd int `json:"diff_add"`
		}
		e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id, nil, &sess)
		if sess.DiffAdd == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("diff_add = %d, want %d", sess.DiffAdd, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReviewDiscard(t *testing.T) {
	e, id, worktree := newReviewEnv(t)

	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/discard", nil, nil); status != http.StatusNoContent {
		t.Fatalf("POST /discard = %d, want 204", status)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree still present at %s (err %v)", worktree, err)
	}
	// Everything review-shaped now answers 422: there is no worktree left.
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/diff", nil, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("GET /diff after discard = %d, want 422", status)
	}
}

// Every review route answers 422 for a session that runs directly in the
// workspace checkout.
// Every review route answers 422 for a session that runs directly in the
// workspace checkout.
func TestReviewRoutes_NoWorktreeIs422(t *testing.T) {
	e, id := newPlainSessionEnv(t)
	base := "/api/v1/sessions/" + id

	gets := []string{base + "/diff", base + "/diff/file?path=x", base + "/comments", base + "/checkpoints", base + "/patch"}
	for _, path := range gets {
		if status := e.doJSON(e.adminClient, http.MethodGet, path, nil, nil); status != http.StatusUnprocessableEntity {
			t.Errorf("GET %s = %d, want 422", path, status)
		}
	}
	posts := []struct {
		path string
		body any
	}{
		{base + "/comments", map[string]any{"path": "a", "line": 1, "body": "b"}},
		{base + "/review", nil},
		{base + "/commit", map[string]any{"message": "m"}},
		{base + "/pr", map[string]any{"title": "t"}},
		{base + "/rewind", map[string]any{"checkpoint_id": "cp"}},
		{base + "/discard", nil},
	}
	for _, p := range posts {
		if status := e.doJSON(e.adminClient, http.MethodPost, p.path, p.body, nil); status != http.StatusUnprocessableEntity {
			t.Errorf("POST %s = %d, want 422", p.path, status)
		}
	}
}

// A review route on a session the caller cannot see answers 404, never 422.
// A review route on a session the caller cannot see answers 404, never 422.
func TestReviewRoutes_InvisibleSessionIs404(t *testing.T) {
	e, id, _ := newReviewEnv(t)
	_, other := e.memberClient("other@example.com")

	if status := e.doJSON(other, http.MethodGet, "/api/v1/sessions/"+id+"/diff", nil, nil); status != http.StatusNotFound {
		t.Fatalf("GET /diff as another member = %d, want 404", status)
	}
	if status := e.doJSON(other, http.MethodGet, "/api/v1/sessions/nope/checkpoints", nil, nil); status != http.StatusNotFound {
		t.Fatalf("GET /checkpoints for a missing session = %d, want 404", status)
	}
}

// The workspaces API accepts and reports base_branch and auto_checkpoint.
// The workspaces API accepts and reports base_branch and auto_checkpoint.
func TestWorkspacesAPI_ReviewFields(t *testing.T) {
	e := newEnv(t)

	var created struct {
		ID             string `json:"id"`
		BaseBranch     string `json:"base_branch"`
		AutoCheckpoint bool   `json:"auto_checkpoint"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "review-ws", "source": "empty", "default_profile_id": "interactive",
		"worktrees": true, "base_branch": "main",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /workspaces = %d, want 201", status)
	}
	if created.BaseBranch != "main" || !created.AutoCheckpoint {
		t.Fatalf("workspace = %+v, want base_branch main and auto_checkpoint on by default", created)
	}

	var patched struct {
		BaseBranch     string `json:"base_branch"`
		AutoCheckpoint bool   `json:"auto_checkpoint"`
	}
	status = e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/workspaces/"+created.ID, map[string]any{
		"base_branch": "develop", "auto_checkpoint": false,
	}, &patched)
	if status != http.StatusOK {
		t.Fatalf("PATCH /workspaces/{id} = %d, want 200", status)
	}
	if patched.BaseBranch != "develop" || patched.AutoCheckpoint {
		t.Fatalf("patched workspace = %+v", patched)
	}
}
