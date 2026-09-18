package sessions

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/gitops"
)

const testGitWorkspaceID = "ws-git"

// gitInTestRepo runs one git command directly (test setup only, never
// through internal/gitops) under a fixed identity.
func gitInTestRepo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.name=Test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// seedGitWorkspace creates a real git repository with one commit and
// registers it as a worktree-enabled, auto-checkpointing workspace. It also
// points HOME at a throwaway directory so no global git config leaks into
// the commands internal/gitops runs.
// seedGitWorkspace creates a real git repository with one commit and
// registers it as a worktree-enabled, auto-checkpointing workspace. It also
// points HOME at a throwaway directory so no global git config leaks into
// the commands internal/gitops runs.
func seedGitWorkspace(t *testing.T, repos Repos) domain.Workspace {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	gitInTestRepo(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	gitInTestRepo(t, dir, "add", "-A")
	gitInTestRepo(t, dir, "commit", "-q", "-m", "init")

	now := time.Now()
	ws := domain.Workspace{
		ID: testGitWorkspaceID, Name: "git-ws", Path: dir, DefaultProfileID: "interactive",
		Worktrees: true, AutoCheckpoint: true, Source: domain.WorkspaceSourcePath,
		State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now,
	}
	if err := repos.Workspaces.Create(context.Background(), ws); err != nil {
		t.Fatalf("create git workspace: %v", err)
	}
	return ws
}

// createOnGitWorkspace starts a session on the seeded git workspace as the
// admin (who holds the only seeded Claude token) and waits for it to settle.
// createOnGitWorkspace starts a session on the seeded git workspace as the
// admin (who holds the only seeded Claude token) and waits for it to settle.
func createOnGitWorkspace(t *testing.T, svc *Service, repos Repos, title string) domain.Session {
	t.Helper()
	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testGitWorkspaceID, ProfileID: "interactive", Title: title, Prompt: "go",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess
}

func adminActor() Actor { return Actor{UserID: testAdminID, IsAdmin: true} }

// waitForCheckpoint polls until the session's pump has recorded at least one
// checkpoint, returning them newest first.
// waitForCheckpoint polls until the session's pump has recorded at least one
// checkpoint, returning them newest first.
func waitForCheckpoint(t *testing.T, repos Repos, id string) []domain.Checkpoint {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		cps, err := repos.Checkpoints.ListBySession(context.Background(), id)
		if err != nil {
			t.Fatalf("list checkpoints: %v", err)
		}
		if len(cps) > 0 {
			return cps
		}
		if time.Now().After(deadline) {
			t.Fatal("no checkpoint recorded after a turn that changed a file")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForDiffStats polls until the session row carries the expected diff
// counters, which the pump writes after the checkpoint.
// waitForDiffStats polls until the session row carries the expected diff
// counters, which the pump writes after the checkpoint.
func waitForDiffStats(t *testing.T, repos Repos, id string, add, del int) domain.Session {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		sess, err := repos.Sessions.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get session: %v", err)
		}
		if sess.DiffAdd == add && sess.DiffDel == del {
			return *sess
		}
		if time.Now().After(deadline) {
			t.Fatalf("diff stats = +%d -%d, want +%d -%d", sess.DiffAdd, sess.DiffDel, add, del)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Create on a worktree-enabled workspace makes the worktree and its branch,
// and runs the CLI with the worktree as its cwd.
// Create on a worktree-enabled workspace makes the worktree and its branch,
// and runs the CLI with the worktree as its cwd.
func TestCreate_WorktreeWorkspaceCreatesWorktreeAndBranch(t *testing.T) {
	svc, repos, h := newService(t, createResult(1))
	ws := seedGitWorkspace(t, repos)

	sess := createOnGitWorkspace(t, svc, repos, "Add a version flag")
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	stored, err := repos.Sessions.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	wantDir := filepath.Join(ws.Path, ".styr", "worktrees", sess.ID)
	if stored.Worktree != wantDir {
		t.Fatalf("worktree = %q, want %q", stored.Worktree, wantDir)
	}
	if info, err := os.Stat(wantDir); err != nil || !info.IsDir() {
		t.Fatalf("worktree directory not created at %s: %v", wantDir, err)
	}
	wantBranch := "styr/" + sess.ID[:8] + "-add-a-version-flag"
	if stored.Branch != wantBranch {
		t.Fatalf("branch = %q, want %q", stored.Branch, wantBranch)
	}
	if stored.BaseRef == "" {
		t.Fatal("base_ref is empty, want the commit the worktree started from")
	}
	branches := gitInTestRepo(t, ws.Path, "branch", "--list", wantBranch)
	if !strings.Contains(branches, wantBranch) {
		t.Fatalf("branch %q not present in the repo: %q", wantBranch, branches)
	}

	if len(h.Procs) == 0 {
		t.Fatal("no process started")
	}
	if got := h.Procs[0].Spec.Cwd; got != wantDir {
		t.Fatalf("StartSpec.Cwd = %q, want the worktree %q", got, wantDir)
	}
}

// After a turn that changed a file, the checkpoint row exists and the diff
// stats on the session row are non-zero.
// After a turn that changed a file, the checkpoint row exists and the diff
// stats on the session row are non-zero.
func TestAfterResult_CheckpointAndDiffStats(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	seedGitWorkspace(t, repos)
	ctx := context.Background()

	sess := createOnGitWorkspace(t, svc, repos, "checkpoint me")
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	// The fake harness cannot edit files, so the test writes into the
	// worktree itself and then drives a second turn.
	stored, err := repos.Sessions.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stored.Worktree, "new.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("write file in worktree: %v", err)
	}
	if err := svc.Send(ctx, adminActor(), sess.ID, "next"); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	cps := waitForCheckpoint(t, repos, sess.ID)
	if cps[0].CommitSHA == "" || !strings.HasPrefix(cps[0].Summary, "styr: checkpoint after turn") {
		t.Fatalf("checkpoint = %+v", cps[0])
	}

	waitForDiffStats(t, repos, sess.ID, 2, 0)
}

// Diff and FileDiff reflect a change made in the worktree.
// Diff and FileDiff reflect a change made in the worktree.
func TestDiffAndFileDiff(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	seedGitWorkspace(t, repos)
	ctx := context.Background()

	sess := createOnGitWorkspace(t, svc, repos, "diff me")
	waitForState(t, repos, sess.ID, domain.SessionOpen)
	stored, _ := repos.Sessions.Get(ctx, sess.ID)
	if err := os.WriteFile(filepath.Join(stored.Worktree, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	diff, err := svc.Diff(ctx, adminActor(), sess.ID)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff.Branch != stored.Branch || diff.BaseRef != stored.BaseRef {
		t.Fatalf("diff = %+v, want branch %q base %q", diff, stored.Branch, stored.BaseRef)
	}
	if !diff.Dirty {
		t.Fatal("diff.Dirty = false, want true with an uncommitted change")
	}
	if diff.TotalAdd != 1 || diff.TotalDel != 0 {
		t.Fatalf("totals = +%d -%d, want +1 -0", diff.TotalAdd, diff.TotalDel)
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "README.md" || diff.Files[0].Status != 'M' {
		t.Fatalf("files = %+v, want one modified README.md", diff.Files)
	}

	fd, err := svc.FileDiff(ctx, adminActor(), sess.ID, "README.md")
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	var added string
	for _, h := range fd.Hunks {
		for _, l := range h.Lines {
			if l.Type == gitops.LineAdd {
				added = l.Text
			}
		}
	}
	if added != "world" {
		t.Fatalf("added line = %q, want %q", added, "world")
	}
}

// AddComment then SendReview sends the formatted review message and marks
// the comments sent.
// AddComment then SendReview sends the formatted review message and marks
// the comments sent.
func TestSendReview_FormatsAndMarksSent(t *testing.T) {
	svc, repos, h := newService(t, createResult(1), createResult(1))
	seedGitWorkspace(t, repos)
	ctx := context.Background()

	sess := createOnGitWorkspace(t, svc, repos, "review me")
	waitForState(t, repos, sess.ID, domain.SessionOpen)
	// The review send starts a turn; a file change gives that turn an
	// observable end (the diff counters) to wait for below, so the test never
	// finishes while the worktree bookkeeping is still running.
	stored, _ := repos.Sessions.Get(ctx, sess.ID)
	if err := os.WriteFile(filepath.Join(stored.Worktree, "note.txt"), []byte("note\n"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	if _, err := svc.AddComment(ctx, adminActor(), sess.ID, CommentInput{
		Path: "README.md", Line: 2, Side: domain.ReviewSideNew, Body: "explain this line",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := svc.SendReview(ctx, adminActor(), sess.ID); err != nil {
		t.Fatalf("SendReview: %v", err)
	}

	sent := h.Procs[0].Sent
	if len(sent) < 2 {
		t.Fatalf("sent = %+v, want the prompt plus the review message", sent)
	}
	last := sent[len(sent)-1].Text
	if !strings.Contains(last, "README.md:2 (new): explain this line") {
		t.Fatalf("review message = %q", last)
	}
	if !strings.HasPrefix(last, "Review comments on your changes") {
		t.Fatalf("review message is missing its header: %q", last)
	}

	waitForDiffStats(t, repos, sess.ID, 1, 0)

	comments, err := repos.ReviewComments.ListBySession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 || comments[0].SentAt == nil {
		t.Fatalf("comments = %+v, want one marked sent", comments)
	}

	// A second review with nothing unsent is refused.
	if err := svc.SendReview(ctx, adminActor(), sess.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("SendReview (nothing unsent) = %v, want ErrInvalid", err)
	}
}

// Commit folds the checkpoints into one commit authored by the actor.
