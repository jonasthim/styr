// worktree_test.go covers Create with CreateInput.WorktreePath: starting a session on an
// existing worktree instead of a fresh one, so a pipeline step can continue where the previous
// step left off (see docs/superpowers/plans/2026-09-19-styr-v0.5-pipelines.md, "worktree:
// shared"). The plain worktree-creation path is covered in review_test.go.
package sessions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
)

// Create with WorktreePath set to another session's worktree reuses that worktree instead of
// making a fresh one: same cwd, same branch and base ref, and the reusing session's diff
// already shows the first session's file.
func TestCreate_WorktreePathReusesExistingWorktree(t *testing.T) {
	svc, repos, h := newService(t, createResult(1), createResult(1))
	seedGitWorkspace(t, repos)
	ctx := context.Background()

	a := createOnGitWorkspace(t, svc, repos, "step one")
	waitForState(t, repos, a.ID, domain.SessionOpen)
	storedA, err := repos.Sessions.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storedA.Worktree, "from-a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file in a's worktree: %v", err)
	}
	// Turn the file write into a checkpoint, matching what a real pipeline step (which does
	// its own edits through the CLI, not a direct write like this test) would leave behind.
	if err := svc.Send(ctx, adminActor(), a.ID, "next"); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForState(t, repos, a.ID, domain.SessionOpen)
	waitForCheckpoint(t, repos, a.ID)

	owner := testAdminID
	b, err := svc.Create(ctx, adminActor(), CreateInput{
		WorkspaceID: testGitWorkspaceID, ProfileID: "interactive", Title: "step two", Prompt: "go",
		Origin: domain.OriginUI, Owner: &owner, WorktreePath: storedA.Worktree,
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	waitForState(t, repos, b.ID, domain.SessionOpen)
	storedB, err := repos.Sessions.Get(ctx, b.ID)
	if err != nil {
		t.Fatalf("get b: %v", err)
	}

	if storedB.Worktree != storedA.Worktree {
		t.Fatalf("b.Worktree = %q, want a's worktree %q", storedB.Worktree, storedA.Worktree)
	}
	if storedB.Branch != storedA.Branch {
		t.Fatalf("b.Branch = %q, want a's branch %q", storedB.Branch, storedA.Branch)
	}
	if storedB.BaseRef != storedA.BaseRef {
		t.Fatalf("b.BaseRef = %q, want a's base ref %q", storedB.BaseRef, storedA.BaseRef)
	}
	if !storedB.WorktreeShared {
		t.Fatal("b.WorktreeShared = false, want true")
	}
	if storedA.WorktreeShared {
		t.Fatal("a.WorktreeShared = true, want false (a created its own worktree)")
	}

	// The process ran with the shared worktree as its cwd.
	if len(h.Procs) < 2 {
		t.Fatalf("processes started = %d, want at least 2", len(h.Procs))
	}
	if got := h.Procs[len(h.Procs)-1].Spec.Cwd; got != storedA.Worktree {
		t.Fatalf("b's StartSpec.Cwd = %q, want the shared worktree %q", got, storedA.Worktree)
	}

	// b's diff already carries a's checkpointed file.
	diff, err := svc.Diff(ctx, adminActor(), b.ID)
	if err != nil {
		t.Fatalf("Diff b: %v", err)
	}
	found := false
	for _, f := range diff.Files {
		if f.Path == "from-a.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("b's diff = %+v, want it to include a's from-a.txt", diff.Files)
	}
}

// Discard refuses (ErrConflict) while another session sharing the worktree is still open,
// running or waiting, and succeeds once that session has closed — removing the worktree only
// on the last user.
func TestDiscard_RefusesWhileSharedWorktreeStillInUse(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1), createResult(1))
	ws := seedGitWorkspace(t, repos)
	ctx := context.Background()

	a := createOnGitWorkspace(t, svc, repos, "step one")
	waitForState(t, repos, a.ID, domain.SessionOpen)
	storedA, err := repos.Sessions.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get a: %v", err)
	}

	owner := testAdminID
	b, err := svc.Create(ctx, adminActor(), CreateInput{
		WorkspaceID: testGitWorkspaceID, ProfileID: "interactive", Title: "step two", Prompt: "go",
		Origin: domain.OriginUI, Owner: &owner, WorktreePath: storedA.Worktree,
	})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	waitForState(t, repos, b.ID, domain.SessionOpen)

	// b is open (idle, but still a live user of the worktree): a cannot discard it.
	if err := svc.Discard(ctx, adminActor(), a.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Discard(a) while b is open = %v, want ErrConflict", err)
	}

	// b closes: now a is the only session left on the worktree, and can discard it.
	if err := svc.Close(ctx, adminActor(), b.ID); err != nil {
		t.Fatalf("close b: %v", err)
	}
	waitForState(t, repos, b.ID, domain.SessionClosed)
	if err := svc.Discard(ctx, adminActor(), a.ID); err != nil {
		t.Fatalf("Discard(a) after b closed: %v", err)
	}
	if _, err := os.Stat(storedA.Worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree directory still present at %s (err %v)", storedA.Worktree, err)
	}
	branches := gitInTestRepo(t, ws.Path, "branch", "--list", storedA.Branch)
	if branches != "" {
		t.Fatalf("branch %q still present: %q", storedA.Branch, branches)
	}
}

// CreateInput.WorktreePath outside the workspace's worktrees directory, or pointing at a
// directory that is not a registered git worktree, is rejected with ErrInvalid.
func TestCreate_WorktreePathInvalid(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	ws := seedGitWorkspace(t, repos)
	ctx := context.Background()
	owner := testAdminID

	cases := map[string]string{
		"outside the workspace entirely":                                    t.TempDir(),
		"the workspace checkout itself":                                     ws.Path,
		"a directory under worktrees/ that was never created as a worktree": filepath.Join(ws.Path, ".styr", "worktrees", "never-existed"),
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(ctx, adminActor(), CreateInput{
				WorkspaceID: testGitWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "go",
				Origin: domain.OriginUI, Owner: &owner, WorktreePath: path,
			})
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Create(WorktreePath=%q) = %v, want ErrInvalid", path, err)
			}
		})
	}
}

// A WorktreePath on a workspace with worktrees disabled is also ErrInvalid, even though the
// path itself might otherwise look plausible.
func TestCreate_WorktreePathWithoutWorktreesEnabledIsInvalid(t *testing.T) {
	svc, _, _ := newService(t)
	owner := testAdminID

	_, err := svc.Create(context.Background(), adminActor(), CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "go",
		Origin: domain.OriginUI, Owner: &owner, WorktreePath: "/some/path",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create(WorktreePath) on a non-worktree workspace = %v, want ErrInvalid", err)
	}
}
