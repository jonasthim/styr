package api_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// gitFixture runs one git command for test fixture setup under a fixed
// identity and a throwaway HOME.
func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// seedGitWorkspace registers a real one-commit git repository as a
// worktree-enabled, auto-checkpointing workspace.
// seedGitWorkspace registers a real one-commit git repository as a
// worktree-enabled, auto-checkpointing workspace.
func (e *testEnv) seedGitWorkspace() domain.Workspace {
	e.t.Helper()
	e.t.Setenv("HOME", e.t.TempDir())

	dir := e.t.TempDir()
	gitFixture(e.t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		e.t.Fatalf("write README: %v", err)
	}
	gitFixture(e.t, dir, "add", "-A")
	gitFixture(e.t, dir, "commit", "-q", "-m", "init")

	now := time.Now()
	ws := domain.Workspace{
		ID: "ws-git", Name: "ws-git", Path: dir, DefaultProfileID: "interactive",
		Worktrees: true, AutoCheckpoint: true, Source: domain.WorkspaceSourcePath,
		State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now,
	}
	if err := e.workspaces.Create(context.Background(), ws); err != nil {
		e.t.Fatalf("create git workspace: %v", err)
	}
	return ws
}

// newReviewEnv starts an admin-owned session on a worktree workspace and
// returns the environment, the session id and its worktree path.
// newReviewEnv starts an admin-owned session on a worktree workspace and
// returns the environment, the session id and its worktree path.
func newReviewEnv(t *testing.T) (*testEnv, string, string) {
	t.Helper()
	e := newEnv(t, createResultStep(), createResultStep())
	ws := e.seedGitWorkspace()
	e.seedToken(e.adminID)

	var created struct {
		ID       string `json:"id"`
		Worktree string `json:"worktree"`
		Branch   string `json:"branch"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "review me", "prompt": "hello",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}
	if created.Worktree == "" || created.Branch == "" {
		t.Fatalf("session = %+v, want a worktree and branch", created)
	}
	// The first turn's checkpoint and diff bookkeeping run before the
	// session settles on "open", so waiting here keeps the test's own writes
	// out of that turn.
	waitForSessionState(t, e, created.ID, "open")
	return e, created.ID, created.Worktree
}

// waitForSessionState polls GET /sessions/{id} until it reports want.
// waitForSessionState polls GET /sessions/{id} until it reports want.
func waitForSessionState(t *testing.T, e *testEnv, id, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var sess struct {
			State string `json:"state"`
		}
		e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id, nil, &sess)
		if sess.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session state = %q, want %q", sess.State, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// newPlainSessionEnv starts a session on a workspace without worktrees, for
// the 422 "session has no worktree" cases.
// newPlainSessionEnv starts a session on a workspace without worktrees, for
// the 422 "session has no worktree" cases.
func newPlainSessionEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	e := newEnv(t, createResultStep())
	ws := e.seedWorkspace("interactive")
	e.seedToken(e.adminID)

	var created struct {
		ID string `json:"id"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "plain", "prompt": "hello",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}
	waitForSessionState(t, e, created.ID, "open")
	return e, created.ID
}

func TestReviewDiffAndFileDiff(t *testing.T) {
	e, id, worktree := newReviewEnv(t)
	if err := os.WriteFile(filepath.Join(worktree, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	var diff struct {
		BaseRef string `json:"base_ref"`
		Branch  string `json:"branch"`
		Files   []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
			Add    int    `json:"add"`
		} `json:"files"`
		TotalAdd int  `json:"total_add"`
		Dirty    bool `json:"dirty"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/diff", nil, &diff); status != http.StatusOK {
		t.Fatalf("GET /diff = %d, want 200", status)
	}
	if diff.BaseRef == "" || diff.Branch == "" || !diff.Dirty {
		t.Fatalf("diff = %+v", diff)
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "README.md" || diff.Files[0].Status != "M" {
		t.Fatalf("files = %+v, want one modified README.md", diff.Files)
	}
	if diff.TotalAdd != 1 {
		t.Fatalf("total_add = %d, want 1", diff.TotalAdd)
	}

	var fd struct {
		Path  string `json:"path"`
		Hunks []struct {
			Lines []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"lines"`
		} `json:"hunks"`
	}
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/diff/file?path=README.md", nil, &fd)
	if status != http.StatusOK {
		t.Fatalf("GET /diff/file = %d, want 200", status)
	}
	found := false
	for _, h := range fd.Hunks {
		for _, l := range h.Lines {
			if l.Type == "add" && l.Text == "world" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("file diff = %+v, want an added line %q", fd, "world")
	}

	// No path: 422.
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/diff/file", nil, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("GET /diff/file without path = %d, want 422", status)
	}
}

// A binary file is flagged on both the diff listing and the per-file diff,
// and the per-file diff always reports whether it was truncated.
// A binary file is flagged on both the diff listing and the per-file diff,
// and the per-file diff always reports whether it was truncated.
func TestReviewDiff_BinaryAndTruncatedFlags(t *testing.T) {
	e, id, worktree := newReviewEnv(t)
	if err := os.WriteFile(filepath.Join(worktree, "logo.bin"), []byte{0x00, 0x01, 0x02, 'a'}, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}

	var diff struct {
		Files []struct {
			Path   string `json:"path"`
			Binary bool   `json:"binary"`
		} `json:"files"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/diff", nil, &diff); status != http.StatusOK {
		t.Fatalf("GET /diff = %d, want 200", status)
	}
	var found bool
	for _, f := range diff.Files {
		if f.Path == "logo.bin" {
			found = true
			if !f.Binary {
				t.Fatalf("logo.bin binary = false, want true")
			}
		}
	}
	if !found {
		t.Fatalf("files = %+v, want logo.bin", diff.Files)
	}

	var fd struct {
		Binary    bool `json:"binary"`
		Truncated bool `json:"truncated"`
	}
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/diff/file?path=logo.bin", nil, &fd)
	if status != http.StatusOK {
		t.Fatalf("GET /diff/file = %d, want 200", status)
	}
	if !fd.Binary || fd.Truncated {
		t.Fatalf("file diff = %+v, want binary and not truncated", fd)
	}
}

func TestReviewPatchDownload(t *testing.T) {
	e, id, worktree := newReviewEnv(t)
	if err := os.WriteFile(filepath.Join(worktree, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	gitFixture(t, worktree, "add", "-A")
	gitFixture(t, worktree, "commit", "-q", "-m", "wip")

	req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/api/v1/sessions/"+id+"/patch", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := e.adminClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /patch = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/x-diff") {
		t.Fatalf("content type = %q, want text/x-diff", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "+world") {
		t.Fatalf("patch = %q, want the added line", body)
	}
}

func TestReviewComments_CRUDAndSend(t *testing.T) {
	e, id, worktree := newReviewEnv(t)
	// The review send starts a turn; a file change gives that turn an
	// observable end (the diff counters) to wait for below, so the test
	// never finishes while the worktree bookkeeping is still running.
	if err := os.WriteFile(filepath.Join(worktree, "note.txt"), []byte("note\n"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	var created struct {
		ID         string  `json:"id"`
		Path       string  `json:"path"`
		Line       int     `json:"line"`
		Side       string  `json:"side"`
		AuthorID   string  `json:"author_id"`
		AuthorName string  `json:"author_name"`
		SentAt     *string `json:"sent_at"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/comments", map[string]any{
		"path": "README.md", "line": 2, "side": "new", "body": "explain this",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /comments = %d, want 201", status)
	}
	if created.ID == "" || created.Side != "new" || created.Line != 2 || created.SentAt != nil {
		t.Fatalf("comment = %+v", created)
	}
	if created.AuthorID == "" || created.AuthorName == "" {
		t.Fatalf("comment author = %q/%q, want both an id and a resolved name", created.AuthorID, created.AuthorName)
	}

	// Empty body: 422.
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/comments", map[string]any{
		"path": "README.md", "line": 2, "body": "",
	}, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /comments with an empty body = %d, want 422", status)
	}

	var list []struct {
		ID         string `json:"id"`
		AuthorName string `json:"author_name"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/sessions/"+id+"/comments", nil, &list); status != http.StatusOK {
		t.Fatalf("GET /comments = %d, want 200", status)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("comments = %+v, want the one created", list)
	}
	if list[0].AuthorName != created.AuthorName {
		t.Fatalf("author_name = %q, want %q", list[0].AuthorName, created.AuthorName)
	}

	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/review", nil, nil); status != http.StatusAccepted {
		t.Fatalf("POST /review = %d, want 202", status)
	}
	sent := e.harness.Procs[0].Sent
	if !strings.Contains(sent[len(sent)-1].Text, "README.md:2 (new): explain this") {
		t.Fatalf("review message = %q", sent[len(sent)-1].Text)
	}
	waitForSessionDiff(t, e, id, 1)

	// Nothing unsent left: 422.
	if status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/sessions/"+id+"/review", nil, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /review with nothing unsent = %d, want 422", status)
	}

	if status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/sessions/"+id+"/comments/"+created.ID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("DELETE /comments/{cid} = %d, want 204", status)
	}
	if status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/sessions/"+id+"/comments/"+created.ID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("DELETE /comments/{cid} again = %d, want 404", status)
	}
}
