package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// seedTestUser creates a minimal user row so an owned workspace's
// owner_user_id foreign key (REFERENCES users(id)) is satisfiable.
func seedTestUser(t *testing.T, d *DB, id string) {
	t.Helper()
	now := time.Now()
	u := domain.User{ID: id, Issuer: "test", Subject: id, Role: domain.RoleMember, CreatedAt: now, LastLoginAt: now}
	if err := NewUsers(d).Create(context.Background(), u); err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func newTestWorkspace(name string) domain.Workspace {
	now := time.Now()
	return domain.Workspace{
		ID:               uuid.NewString(),
		Name:             name,
		Path:             "/srv/" + name,
		Source:           domain.WorkspaceSourcePath,
		DefaultProfileID: "interactive",
		Worktrees:        true,
		State:            domain.WorkspaceReady,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func newTestOwnedWorkspace(owner, name string) domain.Workspace {
	ws := newTestWorkspace(name)
	ws.OwnerID = &owner
	ws.Source = domain.WorkspaceSourceEmpty
	ws.Managed = true
	return ws
}

func TestWorkspaces_CreateGetListUpdateDelete(t *testing.T) {
	ctx := context.Background()
	workspaces := NewWorkspaces(testOpenDB(t))

	w := newTestWorkspace("proj-a")
	if err := workspaces.Create(ctx, w); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := workspaces.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != w.Name || got.Path != w.Path || !got.Worktrees || got.OwnerID != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, w)
	}
	if got.Source != domain.WorkspaceSourcePath || got.State != domain.WorkspaceReady {
		t.Fatalf("Get source/state = %q/%q, want path/ready", got.Source, got.State)
	}

	list, err := workspaces.ListVisible(ctx, "", true)
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListVisible len = %d, want 1", len(list))
	}

	w.Name = "proj-a-renamed"
	w.Worktrees = false
	w.UpdatedAt = time.Now()
	if err := workspaces.Update(ctx, w); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = workspaces.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Name != "proj-a-renamed" || got.Worktrees {
		t.Fatalf("Get after update = %+v", got)
	}

	if err := workspaces.SetState(ctx, w.ID, domain.WorkspaceFailed, "boom", time.Now()); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	got, err = workspaces.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get after SetState: %v", err)
	}
	if got.State != domain.WorkspaceFailed || got.Error != "boom" {
		t.Fatalf("Get after SetState = %+v", got)
	}

	if err := workspaces.Delete(ctx, w.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := workspaces.Get(ctx, w.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestWorkspaces_CreateDuplicateNameForSameOwnerConflicts(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	workspaces := NewWorkspaces(database)
	seedTestUser(t, database, "u1")

	w1 := newTestWorkspace("dup")
	if err := workspaces.Create(ctx, w1); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	w2 := newTestWorkspace("dup")
	if err := workspaces.Create(ctx, w2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate shared name: err = %v, want ErrConflict", err)
	}

	owned1 := newTestOwnedWorkspace("u1", "dup-owned")
	if err := workspaces.Create(ctx, owned1); err != nil {
		t.Fatalf("Create owned first: %v", err)
	}
	owned2 := newTestOwnedWorkspace("u1", "dup-owned")
	if err := workspaces.Create(ctx, owned2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate owned name: err = %v, want ErrConflict", err)
	}
}

func TestWorkspaces_SameNameAllowedForDifferentOwners(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	workspaces := NewWorkspaces(database)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")

	w1 := newTestOwnedWorkspace("u1", "shared-name")
	if err := workspaces.Create(ctx, w1); err != nil {
		t.Fatalf("Create for u1: %v", err)
	}
	w2 := newTestOwnedWorkspace("u2", "shared-name")
	if err := workspaces.Create(ctx, w2); err != nil {
		t.Fatalf("Create for u2 with same name: %v", err)
	}
}

func TestWorkspaces_ListVisible(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	workspaces := NewWorkspaces(database)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")

	shared := newTestWorkspace("shared-ws")
	u1ws := newTestOwnedWorkspace("u1", "u1-ws")
	u2ws := newTestOwnedWorkspace("u2", "u2-ws")
	for _, ws := range []domain.Workspace{shared, u1ws, u2ws} {
		if err := workspaces.Create(ctx, ws); err != nil {
			t.Fatalf("Create %s: %v", ws.Name, err)
		}
	}

	list, err := workspaces.ListVisible(ctx, "u1", false)
	if err != nil {
		t.Fatalf("ListVisible u1: %v", err)
	}
	names := map[string]bool{}
	for _, ws := range list {
		names[ws.Name] = true
	}
	if !names["shared-ws"] || !names["u1-ws"] || names["u2-ws"] {
		t.Fatalf("ListVisible u1 = %+v, want shared-ws and u1-ws only", names)
	}

	adminList, err := workspaces.ListVisible(ctx, "", true)
	if err != nil {
		t.Fatalf("ListVisible admin: %v", err)
	}
	if len(adminList) != 3 {
		t.Fatalf("ListVisible admin len = %d, want 3", len(adminList))
	}
}

// TestWorkspaces_AccessDefaultsToEveryone confirms a workspace created
// without setting Access (every pre-T64 caller, and most tests) round-trips
// as "everyone" rather than an empty string the access CHECK would reject.
func TestWorkspaces_AccessDefaultsToEveryone(t *testing.T) {
	ctx := context.Background()
	workspaces := NewWorkspaces(testOpenDB(t))

	w := newTestWorkspace("access-default")
	if err := workspaces.Create(ctx, w); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := workspaces.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Access != domain.WorkspaceAccessEveryone {
		t.Fatalf("Access = %q, want %q", got.Access, domain.WorkspaceAccessEveryone)
	}
}

// TestWorkspaces_AccessRoundTripsThroughUpdate confirms Update persists a
// changed Access value (e.g. workspaces.Service.SetAccess switching a
// shared workspace to "listed").
func TestWorkspaces_AccessRoundTripsThroughUpdate(t *testing.T) {
	ctx := context.Background()
	workspaces := NewWorkspaces(testOpenDB(t))

	w := newTestWorkspace("access-update")
	if err := workspaces.Create(ctx, w); err != nil {
		t.Fatalf("Create: %v", err)
	}
	w.Access = domain.WorkspaceAccessListed
	w.UpdatedAt = time.Now()
	if err := workspaces.Update(ctx, w); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := workspaces.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Access != domain.WorkspaceAccessListed {
		t.Fatalf("Access after update = %q, want %q", got.Access, domain.WorkspaceAccessListed)
	}
}
