package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestWorkspace(name string) domain.Workspace {
	return domain.Workspace{
		ID:               uuid.NewString(),
		Name:             name,
		Path:             "/srv/" + name,
		DefaultProfileID: "interactive",
		Worktrees:        true,
		CreatedAt:        time.Now(),
	}
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
	if got.Name != w.Name || got.Path != w.Path || !got.Worktrees {
		t.Fatalf("Get = %+v, want matching %+v", got, w)
	}

	list, err := workspaces.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	w.Name = "proj-a-renamed"
	w.Worktrees = false
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

	if err := workspaces.Delete(ctx, w.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := workspaces.Get(ctx, w.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestWorkspaces_CreateDuplicateNameConflicts(t *testing.T) {
	ctx := context.Background()
	workspaces := NewWorkspaces(testOpenDB(t))

	w1 := newTestWorkspace("dup")
	if err := workspaces.Create(ctx, w1); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	w2 := newTestWorkspace("dup")
	if err := workspaces.Create(ctx, w2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate: err = %v, want ErrConflict", err)
	}
}
