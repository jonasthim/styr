package db

import (
	"context"
	"testing"
)

func TestWorkspaceAccess_SetListHasAccess(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	workspaces := NewWorkspaces(database)
	access := NewWorkspaceAccess(database)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")

	ws := newTestWorkspace("shared-listed")
	if err := workspaces.Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// No rows yet.
	list, err := access.List(ctx, ws.ID)
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List (empty) = %+v, want none", list)
	}
	if ok, err := access.HasAccess(ctx, ws.ID, "u1"); err != nil || ok {
		t.Fatalf("HasAccess (empty) = %v, %v, want false, nil", ok, err)
	}

	if err := access.Set(ctx, ws.ID, []string{"u1", "u2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	list, err = access.List(ctx, ws.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || list[0] != "u1" || list[1] != "u2" {
		t.Fatalf("List = %+v, want [u1 u2]", list)
	}
	if ok, err := access.HasAccess(ctx, ws.ID, "u1"); err != nil || !ok {
		t.Fatalf("HasAccess u1 = %v, %v, want true, nil", ok, err)
	}

	// Set replaces the whole list, not merges into it.
	if err := access.Set(ctx, ws.ID, []string{"u2"}); err != nil {
		t.Fatalf("Set (replace): %v", err)
	}
	list, err = access.List(ctx, ws.ID)
	if err != nil {
		t.Fatalf("List (replace): %v", err)
	}
	if len(list) != 1 || list[0] != "u2" {
		t.Fatalf("List (replace) = %+v, want [u2]", list)
	}
	if ok, err := access.HasAccess(ctx, ws.ID, "u1"); err != nil || ok {
		t.Fatalf("HasAccess u1 (replace) = %v, %v, want false, nil", ok, err)
	}

	// Set with an empty list clears it.
	if err := access.Set(ctx, ws.ID, nil); err != nil {
		t.Fatalf("Set (clear): %v", err)
	}
	list, err = access.List(ctx, ws.ID)
	if err != nil {
		t.Fatalf("List (clear): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List (clear) = %+v, want none", list)
	}
}

// TestWorkspaceAccess_CascadesOnWorkspaceDelete confirms the schema's ON
// DELETE CASCADE: deleting the workspace row removes its workspace_access
// rows too, rather than leaving them orphaned.
func TestWorkspaceAccess_CascadesOnWorkspaceDelete(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	workspaces := NewWorkspaces(database)
	access := NewWorkspaceAccess(database)
	seedTestUser(t, database, "u1")

	ws := newTestWorkspace("shared-cascade")
	if err := workspaces.Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := access.Set(ctx, ws.ID, []string{"u1"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := workspaces.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	list, err := access.List(ctx, ws.ID)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after delete = %+v, want none", list)
	}
}
