package workspaces

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/sessions"
)

// newTestService opens a fresh temp DB, seeds member users "u1"/"u2" and an
// admin, and returns a Service plus the repos tests need to seed/inspect
// state directly.
func newTestService(t *testing.T) (svc *Service, repo *db.Workspaces, sessionsRepo *db.Sessions, usersDir string) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	repo = db.NewWorkspaces(database)
	sessionsRepo = db.NewSessions(database)
	users := db.NewUsers(database)
	usersDir = t.TempDir()

	now := time.Now()
	for _, id := range []string{"u1", "u2", "admin"} {
		u := domain.User{ID: id, Issuer: "test", Subject: id, Role: domain.RoleMember, CreatedAt: now, LastLoginAt: now}
		if id == "admin" {
			u.Role = domain.RoleAdmin
		}
		if err := users.Create(context.Background(), u); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc = New(repo, sessionsRepo, events.New(), usersDir, logger)
	return svc, repo, sessionsRepo, usersDir
}

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

// createBareRepoAt initialises a bare repo at dir with one commit on
// "main", pushed from a scratch working clone.
func createBareRepoAt(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir bare repo: %v", err)
	}
	runGitT(t, dir, "init", "--bare", "-b", "main")

	work := t.TempDir()
	runGitT(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitT(t, work, "add", "README.md")
	runGitT(t, work, "commit", "-m", "init")
	runGitT(t, work, "remote", "add", "origin", dir)
	runGitT(t, work, "push", "origin", "main")
}

// newBareRepoWithCommit is createBareRepoAt at a fresh temp path, returning
// the bare repo's directory.
func newBareRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo.git")
	createBareRepoAt(t, dir)
	return dir
}

// waitForState polls repo for id to reach want, failing the test if it
// does not within 10s.
func waitForState(t *testing.T, repo *db.Workspaces, id string, want domain.WorkspaceState) domain.Workspace {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ws, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get workspace: %v", err)
		}
		if ws.State == want {
			return *ws
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace %s: state = %s (error=%q), want %s", id, ws.State, ws.Error, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCreate_Git_ReachesReadyWithClonePresent(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	bare := newBareRepoWithCommit(t)
	actor := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), actor, CreateInput{
		Name: "proj", Source: "git", RepoURL: "file://" + bare, DefaultProfileID: "interactive",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.State != domain.WorkspaceCloning {
		t.Fatalf("initial state = %s, want cloning", ws.State)
	}

	ready := waitForState(t, repo, ws.ID, domain.WorkspaceReady)
	if _, err := os.Stat(filepath.Join(ready.Path, ".git")); err != nil {
		t.Fatalf("clone not present at %s: %v", ready.Path, err)
	}
	if _, err := os.Stat(filepath.Join(ready.Path, "README.md")); err != nil {
		t.Fatalf("cloned file missing: %v", err)
	}
}

func TestCreate_Git_BadURL_Invalid(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}

	_, err := svc.Create(context.Background(), actor, CreateInput{
		Name: "bad", Source: "git", RepoURL: "not-a-url", DefaultProfileID: "interactive",
	})
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("err = %v, want ErrInvalidURL", err)
	}
}

func TestCreate_Git_CloneFailure_MarksFailedAndCleansUp(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}
	missing := filepath.Join(t.TempDir(), "does-not-exist.git")

	ws, err := svc.Create(context.Background(), actor, CreateInput{
		Name: "broken", Source: "git", RepoURL: "file://" + missing, DefaultProfileID: "interactive",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	failed := waitForState(t, repo, ws.ID, domain.WorkspaceFailed)
	if failed.Error == "" {
		t.Fatalf("Error = %q, want non-empty", failed.Error)
	}
	if _, err := os.Stat(failed.Path); !os.IsNotExist(err) {
		t.Fatalf("expected no leftover dir at %s, stat err = %v", failed.Path, err)
	}
}

func TestCreate_Empty_RunsGitInit(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), actor, CreateInput{Name: "scratch", Source: "empty", DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.State != domain.WorkspaceReady {
		t.Fatalf("state = %s, want ready", ws.State)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, ".git")); err != nil {
		t.Fatalf("git init did not run: %v", err)
	}
}

func TestCreate_Path_MemberForbidden(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	actor := sessions.Actor{UserID: "u1"}

	_, err := svc.Create(context.Background(), actor, CreateInput{Name: "shared", Source: "path", Path: dir, DefaultProfileID: "interactive"})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreate_Path_AdminOK(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	admin := sessions.Actor{UserID: "admin", IsAdmin: true}

	ws, err := svc.Create(context.Background(), admin, CreateInput{Name: "shared", Source: "path", Path: dir, DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.OwnerID != nil {
		t.Fatalf("OwnerID = %v, want nil", ws.OwnerID)
	}
	if ws.Managed {
		t.Fatalf("Managed = true, want false")
	}
	if ws.State != domain.WorkspaceReady {
		t.Fatalf("state = %s, want ready", ws.State)
	}
}

func TestCreate_Path_NonGitDir_InvalidPath(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	dir := t.TempDir() // no .git
	admin := sessions.Actor{UserID: "admin", IsAdmin: true}

	_, err := svc.Create(context.Background(), admin, CreateInput{Name: "notgit", Source: "path", Path: dir, DefaultProfileID: "interactive"})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
}

func TestCreate_DuplicateNameSameOwner_NameTaken(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}

	if _, err := svc.Create(context.Background(), actor, CreateInput{Name: "dup", Source: "empty", DefaultProfileID: "interactive"}); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	_, err := svc.Create(context.Background(), actor, CreateInput{Name: "dup", Source: "empty", DefaultProfileID: "interactive"})
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
}

func TestGet_MemberCannotSeeAnotherMembersWorkspace(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	owner := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), owner, CreateInput{Name: "mine", Source: "empty", DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	other := sessions.Actor{UserID: "u2"}
	if _, err := svc.Get(context.Background(), other, ws.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get by other member: err = %v, want ErrNotFound", err)
	}

	// Visible to admin and to the owner.
	admin := sessions.Actor{UserID: "admin", IsAdmin: true}
	if _, err := svc.Get(context.Background(), admin, ws.ID); err != nil {
		t.Fatalf("Get by admin: %v", err)
	}
	if _, err := svc.Get(context.Background(), owner, ws.ID); err != nil {
		t.Fatalf("Get by owner: %v", err)
	}
}

func TestList_SharedPathVisibleToEveryone(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	admin := sessions.Actor{UserID: "admin", IsAdmin: true}
	if _, err := svc.Create(context.Background(), admin, CreateInput{Name: "shared", Source: "path", Path: dir, DefaultProfileID: "interactive"}); err != nil {
		t.Fatalf("Create shared: %v", err)
	}

	member := sessions.Actor{UserID: "u1"}
	list, err := svc.List(context.Background(), member)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "shared" {
		t.Fatalf("List = %+v, want the one shared workspace", list)
	}
}

func TestUpdate_OwnerCanPatchDefaultProfileAndWorktrees(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), actor, CreateInput{Name: "patchme", Source: "empty", DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newProfile := "investigate"
	wt := true
	updated, err := svc.Update(context.Background(), actor, ws.ID, UpdateInput{DefaultProfileID: &newProfile, Worktrees: &wt})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.DefaultProfileID != "investigate" || !updated.Worktrees {
		t.Fatalf("Update = %+v", updated)
	}
}

func TestUpdate_ForbiddenForOtherMember(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	owner := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), owner, CreateInput{Name: "notyours", Source: "empty", DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A workspace owned by someone else is invisible to another member
	// entirely, so Update reports ErrNotFound rather than ErrForbidden
	// (matching internal/sessions.Service's getVisible: a resource you
	// cannot see is not found, not merely forbidden).
	other := sessions.Actor{UserID: "u2"}
	if _, err := svc.Update(context.Background(), other, ws.ID, UpdateInput{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update by other member on an invisible workspace: err = %v, want ErrNotFound", err)
	}
}

func TestUpdate_ForbiddenForMemberOnSharedWorkspace(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	admin := sessions.Actor{UserID: "admin", IsAdmin: true}

	ws, err := svc.Create(context.Background(), admin, CreateInput{Name: "shared3", Source: "path", Path: dir, DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A shared (owner-less) workspace is visible to a member, but only its
	// owner (none, here) or an admin may mutate it.
	member := sessions.Actor{UserID: "u1"}
	if _, err := svc.Update(context.Background(), member, ws.ID, UpdateInput{}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Update shared workspace by a member: err = %v, want ErrForbidden", err)
	}
}

func TestDelete_RefusesWithOpenSession(t *testing.T) {
	svc, repo, sessionsRepo, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), actor, CreateInput{Name: "busy", Source: "empty", DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	owner := "u1"
	sess := domain.Session{
		ID: uuid.NewString(), OwnerID: &owner, Title: "t", WorkspaceID: ws.ID, ProfileID: "interactive",
		Harness: "claude", State: domain.SessionRunning, Origin: domain.OriginUI,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := sessionsRepo.Create(context.Background(), sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := svc.Delete(context.Background(), actor, ws.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Delete: err = %v, want ErrConflict", err)
	}
	if _, err := repo.Get(context.Background(), ws.ID); err != nil {
		t.Fatalf("workspace should still exist: %v", err)
	}
}

func TestDelete_RemovesManagedDir(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), actor, CreateInput{Name: "gone", Source: "empty", DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), actor, ws.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("expected managed dir removed, stat err = %v", err)
	}
	if _, err := repo.Get(context.Background(), ws.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestDelete_SharedPath_OnlyRemovesRow(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	admin := sessions.Actor{UserID: "admin", IsAdmin: true}

	ws, err := svc.Create(context.Background(), admin, CreateInput{Name: "shared2", Source: "path", Path: dir, DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(context.Background(), admin, ws.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("shared dir should remain: %v", err)
	}
	if _, err := repo.Get(context.Background(), ws.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestRetry_ClonesAgainAfterFailureIsResolved(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}
	repoDir := filepath.Join(t.TempDir(), "later.git") // does not exist yet: first clone fails

	ws, err := svc.Create(context.Background(), actor, CreateInput{
		Name: "retry-me", Source: "git", RepoURL: "file://" + repoDir, DefaultProfileID: "interactive",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForState(t, repo, ws.ID, domain.WorkspaceFailed)

	createBareRepoAt(t, repoDir) // now make the remote exist

	if _, err := svc.Retry(context.Background(), actor, ws.ID); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	ready := waitForState(t, repo, ws.ID, domain.WorkspaceReady)
	if _, err := os.Stat(filepath.Join(ready.Path, ".git")); err != nil {
		t.Fatalf("clone not present after retry: %v", err)
	}
}

func TestRetry_RefusesWhenNotFailed(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	actor := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(context.Background(), actor, CreateInput{Name: "healthy", Source: "empty", DefaultProfileID: "interactive"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Retry(context.Background(), actor, ws.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Retry on ready workspace: err = %v, want ErrConflict", err)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Repo":               "my-repo",
		"  Weird!!Name__2024  ": "weird-name-2024",
		strings.Repeat("a", 50): strings.Repeat("a", 40),
		"":                      "workspace",
		"already-a-slug":        "already-a-slug",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidRepoURL(t *testing.T) {
	valid := []string{
		"https://example.com/org/repo.git",
		"ssh://git@example.com/org/repo.git",
		"file:///srv/repos/repo.git",
		"git@example.com:org/repo.git",
	}
	for _, u := range valid {
		if !validRepoURL(u) {
			t.Errorf("validRepoURL(%q) = false, want true", u)
		}
	}
	invalid := []string{"", "not-a-url", "ftp://example.com/repo.git", "javascript:alert(1)"}
	for _, u := range invalid {
		if validRepoURL(u) {
			t.Errorf("validRepoURL(%q) = true, want false", u)
		}
	}
}

func TestCloneFailureMessage_RedactsCredentialsAndBounds(t *testing.T) {
	repoURL := "https://user:secret@example.com/org/repo.git"
	out := "Cloning into 'x'...\n\nfatal: could not read Username for 'https://user:secret@example.com': No such device or address\n"

	msg := cloneFailureMessage(out, repoURL)
	if strings.Contains(msg, "secret") {
		t.Fatalf("message leaks credential: %q", msg)
	}
	if len(msg) > maxErrorLen {
		t.Fatalf("message len = %d, want <= %d", len(msg), maxErrorLen)
	}

	long := cloneFailureMessage(strings.Repeat("x", maxErrorLen*2), "")
	if len(long) != maxErrorLen {
		t.Fatalf("truncated message len = %d, want %d", len(long), maxErrorLen)
	}
}

// Create defaults auto_checkpoint on and carries base_branch through, and
// Update patches both.
func TestCreateAndUpdate_ReviewFields(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	ctx := context.Background()
	actor := sessions.Actor{UserID: "u1"}

	ws, err := svc.Create(ctx, actor, CreateInput{
		Name: "review", Source: "empty", DefaultProfileID: "interactive",
		Worktrees: true, BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.BaseBranch != "main" {
		t.Fatalf("base branch = %q, want main", ws.BaseBranch)
	}
	if !ws.AutoCheckpoint {
		t.Fatal("auto checkpoint = false, want the default on")
	}
	stored, err := repo.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.BaseBranch != "main" || !stored.AutoCheckpoint {
		t.Fatalf("stored workspace = %+v", stored)
	}

	off := false
	base := "develop"
	updated, err := svc.Update(ctx, actor, ws.ID, UpdateInput{BaseBranch: &base, AutoCheckpoint: &off})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.BaseBranch != "develop" || updated.AutoCheckpoint {
		t.Fatalf("updated workspace = %+v", updated)
	}
}

// An explicit auto_checkpoint: false at create time is honoured.
func TestCreate_AutoCheckpointExplicitlyOff(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	off := false
	ws, err := svc.Create(context.Background(), sessions.Actor{UserID: "u1"}, CreateInput{
		Name: "nocp", Source: "empty", DefaultProfileID: "interactive", AutoCheckpoint: &off,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.AutoCheckpoint {
		t.Fatal("auto checkpoint = true, want false")
	}
}
