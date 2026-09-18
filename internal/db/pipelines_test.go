package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestPipeline(workspaceID, name string) domain.Pipeline {
	now := time.Now()
	return domain.Pipeline{
		ID:          uuid.NewString(),
		Name:        name,
		WorkspaceID: workspaceID,
		YAML:        "name: " + name + "\nworkspace: styr\nsteps:\n  - id: a\n    template: t\n",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestPipelines_CreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database) // seeds workspace ws1
	pipelines := NewPipelines(database)

	pl := newTestPipeline("ws1", "fix-ci")
	if err := pipelines.Create(ctx, pl); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := pipelines.Get(ctx, pl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != pl.Name || got.WorkspaceID != pl.WorkspaceID || got.YAML != pl.YAML || got.OwnerID != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, pl)
	}

	pl.YAML = "name: fix-ci\nworkspace: styr\nsteps:\n  - id: a\n    template: t2\n"
	pl.UpdatedAt = time.Now()
	if err := pipelines.Update(ctx, pl); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = pipelines.Get(ctx, pl.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.YAML != pl.YAML {
		t.Fatalf("Get after update = %+v", got)
	}

	if err := pipelines.Delete(ctx, pl.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := pipelines.Get(ctx, pl.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestPipelines_GetUpdateDeleteUnknownID(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	pipelines := NewPipelines(database)

	if _, err := pipelines.Get(ctx, "no-such-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get unknown: err = %v, want ErrNotFound", err)
	}
	if err := pipelines.Update(ctx, newTestPipeline("ws1", "ghost")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update unknown: err = %v, want ErrNotFound", err)
	}
	if err := pipelines.Delete(ctx, "no-such-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete unknown: err = %v, want ErrNotFound", err)
	}
}

func TestPipelines_GetByName(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	pipelines := NewPipelines(database)

	pl := newTestPipeline("ws1", "fix-ci")
	if err := pipelines.Create(ctx, pl); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := pipelines.GetByName(ctx, "ws1", "fix-ci")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ID != pl.ID {
		t.Fatalf("GetByName id = %s, want %s", got.ID, pl.ID)
	}

	if _, err := pipelines.GetByName(ctx, "ws1", "no-such-pipeline"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByName unknown name: err = %v, want ErrNotFound", err)
	}
	if _, err := pipelines.GetByName(ctx, "other-workspace", "fix-ci"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByName wrong workspace: err = %v, want ErrNotFound", err)
	}
}

func TestPipelines_CreateDuplicateNameConflicts(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	seedTestUser(t, database, "u1")
	pipelines := NewPipelines(database)

	shared1 := newTestPipeline("ws1", "dup")
	if err := pipelines.Create(ctx, shared1); err != nil {
		t.Fatalf("Create first shared: %v", err)
	}
	shared2 := newTestPipeline("ws1", "dup")
	if err := pipelines.Create(ctx, shared2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate shared name: err = %v, want ErrConflict", err)
	}

	owned := newTestPipeline("ws1", "dup")
	owned.OwnerID = strPtr("u1")
	if err := pipelines.Create(ctx, owned); err != nil {
		t.Fatalf("Create owned pipeline with same name as shared: %v", err)
	}
}

func TestPipelines_ListVisible(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")
	pipelines := NewPipelines(database)

	shared := newTestPipeline("ws1", "shared-pl")
	u1pl := newTestPipeline("ws1", "u1-pl")
	u1pl.OwnerID = strPtr("u1")
	u2pl := newTestPipeline("ws1", "u2-pl")
	u2pl.OwnerID = strPtr("u2")
	for _, pl := range []domain.Pipeline{shared, u1pl, u2pl} {
		if err := pipelines.Create(ctx, pl); err != nil {
			t.Fatalf("Create %s: %v", pl.Name, err)
		}
	}

	list, err := pipelines.ListVisible(ctx, "u1", false)
	if err != nil {
		t.Fatalf("ListVisible u1: %v", err)
	}
	names := map[string]bool{}
	for _, pl := range list {
		names[pl.Name] = true
	}
	if !names["shared-pl"] || !names["u1-pl"] || names["u2-pl"] {
		t.Fatalf("ListVisible u1 = %+v, want shared-pl and u1-pl only", names)
	}

	adminList, err := pipelines.ListVisible(ctx, "", true)
	if err != nil {
		t.Fatalf("ListVisible admin: %v", err)
	}
	if len(adminList) != 3 {
		t.Fatalf("ListVisible admin len = %d, want 3", len(adminList))
	}
}
