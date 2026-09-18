package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

// Commit folds the checkpoints into one commit authored by the actor.
func TestCommit_FoldsAndUsesActorAsAuthor(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	seedGitWorkspace(t, repos)
	ctx := context.Background()

	sess := createOnGitWorkspace(t, svc, repos, "commit me")
	waitForState(t, repos, sess.ID, domain.SessionOpen)
	stored, _ := repos.Sessions.Get(ctx, sess.ID)
	if err := os.WriteFile(filepath.Join(stored.Worktree, "feature.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	sha, err := svc.Commit(ctx, adminActor(), sess.ID, "feat: add feature")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha == "" {
		t.Fatal("empty sha")
	}
	out := gitInTestRepo(t, stored.Worktree, "log", "-1", "--format=%an <%ae>%n%s")
	if !strings.Contains(out, "Admin <admin@example.com>") {
		t.Fatalf("commit author = %q, want the acting user", out)
	}
	if !strings.Contains(out, "feat: add feature") {
		t.Fatalf("commit subject = %q", out)
	}
	// Exactly one commit on top of the base: the checkpoints were folded in.
	count := strings.TrimSpace(gitInTestRepo(t, stored.Worktree, "rev-list", "--count", stored.BaseRef+"..HEAD"))
	if count != "1" {
		t.Fatalf("commits since base = %s, want 1", count)
	}

	if _, err := svc.Commit(ctx, adminActor(), sess.ID, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Commit(empty message) = %v, want ErrInvalid", err)
	}
}

// CreatePR reports gh being unavailable as the dedicated sentinel (which
// wraps domain.ErrConflict), rather than a generic failure.
// CreatePR reports gh being unavailable as the dedicated sentinel (which
// wraps domain.ErrConflict), rather than a generic failure.
func TestCreatePR_MapsGHUnavailable(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	ws := seedGitWorkspace(t, repos)
	ctx := context.Background()

	// A bare repo alongside the workspace, wired as origin, so Push
	// succeeds and the failure can only come from gh.
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitInTestRepo(t, ws.Path, "init", "--bare", "-q", remote)
	gitInTestRepo(t, ws.Path, "remote", "add", "origin", remote)

	sess := createOnGitWorkspace(t, svc, repos, "pr me")
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	// An empty PATH makes `gh` unfindable; git is resolved from the same
	// PATH, so it is re-added as an absolute-path shim directory holding
	// only git.
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	shim := t.TempDir()
	if err := os.Symlink(gitBin, filepath.Join(shim, "git")); err != nil {
		t.Fatalf("symlink git: %v", err)
	}
	t.Setenv("PATH", shim)

	_, err = svc.CreatePR(ctx, adminActor(), sess.ID, "A title", "A body", "main")
	if !errors.Is(err, ErrGHUnavailable) {
		t.Fatalf("CreatePR = %v, want ErrGHUnavailable", err)
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("CreatePR error does not wrap ErrConflict: %v", err)
	}
}

// Rewind refuses while the session is running and restores a file when it
// is not.
// Rewind refuses while the session is running and restores a file when it
// is not.
func TestRewind_RefusesWhileRunningAndRestores(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	seedGitWorkspace(t, repos)
	ctx := context.Background()

	sess := createOnGitWorkspace(t, svc, repos, "rewind me")
	waitForState(t, repos, sess.ID, domain.SessionOpen)
	stored, _ := repos.Sessions.Get(ctx, sess.ID)

	// Turn 1 leaves a checkpoint containing "v1".
	target := filepath.Join(stored.Worktree, "file.txt")
	if err := os.WriteFile(target, []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	if err := svc.Send(ctx, adminActor(), sess.ID, "turn two"); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	cps := waitForCheckpoint(t, repos, sess.ID)
	// The pump writes the diff stats after the checkpoint; waiting for them
	// keeps the rewind below from racing that last write.
	waitForDiffStats(t, repos, sess.ID, 1, 0)

	// Running: refused.
	if err := repos.Sessions.UpdateState(ctx, sess.ID, domain.SessionRunning); err != nil {
		t.Fatalf("set running: %v", err)
	}
	if err := svc.Rewind(ctx, adminActor(), sess.ID, cps[0].ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Rewind while running = %v, want ErrConflict", err)
	}
	if err := repos.Sessions.UpdateState(ctx, sess.ID, domain.SessionOpen); err != nil {
		t.Fatalf("set open: %v", err)
	}

	// Change the file, then rewind: the checkpointed content comes back.
	if err := os.WriteFile(target, []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	if err := svc.Rewind(ctx, adminActor(), sess.ID, cps[0].ID); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "v1\n" {
		t.Fatalf("file.txt = %q, want the checkpointed v1", got)
	}
}

// Discard removes the worktree directory, clears the session's worktree
// fields and closes it.
// Discard removes the worktree directory, clears the session's worktree
// fields and closes it.
func TestDiscard_RemovesWorktree(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	ws := seedGitWorkspace(t, repos)
	ctx := context.Background()

	sess := createOnGitWorkspace(t, svc, repos, "discard me")
	waitForState(t, repos, sess.ID, domain.SessionOpen)
	stored, _ := repos.Sessions.Get(ctx, sess.ID)
	dir := stored.Worktree

	if err := svc.Discard(ctx, adminActor(), sess.ID); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("worktree directory still present at %s (err %v)", dir, err)
	}
	branches := gitInTestRepo(t, ws.Path, "branch", "--list", stored.Branch)
	if strings.Contains(branches, stored.Branch) {
		t.Fatalf("branch %q still present: %q", stored.Branch, branches)
	}
	after, err := repos.Sessions.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if after.Worktree != "" || after.Branch != "" || after.BaseRef != "" {
		t.Fatalf("session worktree fields not cleared: %+v", after)
	}
	if after.State != domain.SessionClosed {
		t.Fatalf("state = %s, want closed", after.State)
	}

	// Every review operation now reports the session has no worktree.
	if _, err := svc.Diff(ctx, adminActor(), sess.ID); !errors.Is(err, ErrNoWorktree) {
		t.Fatalf("Diff after discard = %v, want ErrNoWorktree", err)
	}
}

// A session on a workspace without worktrees answers every review
// operation with ErrInvalid.
// A session on a workspace without worktrees answers every review
// operation with ErrInvalid.
func TestReviewOperations_NoWorktreeSession(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	ctx := context.Background()
	owner := testAdminID
	sess, err := svc.Create(ctx, adminActor(), CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "plain", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	checks := map[string]error{}
	_, err = svc.Diff(ctx, adminActor(), sess.ID)
	checks["Diff"] = err
	_, err = svc.FileDiff(ctx, adminActor(), sess.ID, "x")
	checks["FileDiff"] = err
	_, err = svc.Comments(ctx, adminActor(), sess.ID)
	checks["Comments"] = err
	_, err = svc.AddComment(ctx, adminActor(), sess.ID, CommentInput{Path: "a", Body: "b"})
	checks["AddComment"] = err
	checks["DeleteComment"] = svc.DeleteComment(ctx, adminActor(), sess.ID, "c")
	checks["SendReview"] = svc.SendReview(ctx, adminActor(), sess.ID)
	_, err = svc.Commit(ctx, adminActor(), sess.ID, "m")
	checks["Commit"] = err
	_, err = svc.CreatePR(ctx, adminActor(), sess.ID, "t", "b", "")
	checks["CreatePR"] = err
	_, err = svc.Checkpoints(ctx, adminActor(), sess.ID)
	checks["Checkpoints"] = err
	checks["Rewind"] = svc.Rewind(ctx, adminActor(), sess.ID, "cp")
	checks["Discard"] = svc.Discard(ctx, adminActor(), sess.ID)
	_, err = svc.Patch(ctx, adminActor(), sess.ID)
	checks["Patch"] = err

	for name, got := range checks {
		if !errors.Is(got, domain.ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, got)
		}
		if got == nil || !strings.Contains(got.Error(), "session has no worktree") {
			t.Errorf("%s message = %v, want %q", name, got, "session has no worktree")
		}
	}
}

// An ExitPlanMode permission request stores its plan markdown on the
// approval and is classified as a read.
// An ExitPlanMode permission request stores its plan markdown on the
// approval and is classified as a read.
func TestHandlePermission_ExitPlanModeCarriesPlan(t *testing.T) {
	const plan = "# Plan\n\n- [ ] step one\n"
	step := fake.Step{
		WaitForDecision: true,
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{
				RequestID: "req-plan", ToolName: "ExitPlanMode",
				Input: json.RawMessage(`{"plan":"# Plan\n\n- [ ] step one\n"}`), Plan: plan,
			}},
			{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
		},
	}
	svc, repos, _ := newService(t, step)
	ctx := context.Background()
	owner := testAdminID
	sess, err := svc.Create(ctx, adminActor(), CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "plan", Prompt: "plan it",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionWaiting)

	pending, err := repos.Approvals.PendingForSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("pending approvals: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].Tool != "ExitPlanMode" {
		t.Fatalf("tool = %q", pending[0].Tool)
	}
	if pending[0].Plan != plan {
		t.Fatalf("plan = %q, want %q", pending[0].Plan, plan)
	}
	if pending[0].Risk != domain.RiskRead {
		t.Fatalf("risk = %q, want read", pending[0].Risk)
	}
}

// branchName slugifies the title, bounds it and always produces a name
// internal/gitops accepts.
// branchName slugifies the title, bounds it and always produces a name
// internal/gitops accepts.
func TestBranchName(t *testing.T) {
	cases := []struct{ id, title, want string }{
		{"0123456789abcdef", "Add a --version flag", "styr/01234567-add-a-version-flag"},
		{"0123456789abcdef", "", "styr/01234567"},
		{"0123456789abcdef", "!!!", "styr/01234567"},
		{"0123456789abcdef", strings.Repeat("a", 60), "styr/01234567-" + strings.Repeat("a", 40)},
	}
	for _, c := range cases {
		if got := branchName(c.id, c.title); got != c.want {
			t.Errorf("branchName(%q, %q) = %q, want %q", c.id, c.title, got, c.want)
		}
	}
}
