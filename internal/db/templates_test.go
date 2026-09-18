package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestTemplate(name string) domain.Template {
	now := time.Now()
	return domain.Template{
		ID:             uuid.NewString(),
		Name:           name,
		WorkspaceID:    "ws1",
		ProfileID:      "investigate",
		TitleTemplate:  "{{ .status }}",
		PromptTemplate: "Investigate {{ .status }}",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// seedTemplateFixtures creates a workspace and profile so a template's
// foreign keys are satisfiable.
func seedTemplateFixtures(t *testing.T, d *DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	ws := domain.Workspace{ID: "ws1", Name: "proj", Path: "/srv/proj", Source: domain.WorkspaceSourcePath,
		DefaultProfileID: "interactive", State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now}
	if err := NewWorkspaces(d).Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
}

func TestTemplates_CreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	templates := NewTemplates(database)

	tpl := newTestTemplate("grafana investigation")
	if err := templates.Create(ctx, tpl); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := templates.Get(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != tpl.Name || got.PromptTemplate != tpl.PromptTemplate || got.OwnerID != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, tpl)
	}

	tpl.Name = "renamed"
	tpl.UpdatedAt = time.Now()
	if err := templates.Update(ctx, tpl); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = templates.Get(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Name != "renamed" {
		t.Fatalf("Get after update = %+v", got)
	}

	if err := templates.Delete(ctx, tpl.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := templates.Get(ctx, tpl.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestTemplates_CreateDuplicateNameConflicts(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	seedTestUser(t, database, "u1")
	templates := NewTemplates(database)

	shared1 := newTestTemplate("dup")
	if err := templates.Create(ctx, shared1); err != nil {
		t.Fatalf("Create first shared: %v", err)
	}
	shared2 := newTestTemplate("dup")
	if err := templates.Create(ctx, shared2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate shared name: err = %v, want ErrConflict", err)
	}

	owned := newTestTemplate("dup")
	owned.OwnerID = strPtr("u1")
	if err := templates.Create(ctx, owned); err != nil {
		t.Fatalf("Create owned template with same name as shared: %v", err)
	}
}

func TestTemplates_ListVisible(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")
	templates := NewTemplates(database)

	shared := newTestTemplate("shared-tpl")
	u1tpl := newTestTemplate("u1-tpl")
	u1tpl.OwnerID = strPtr("u1")
	u2tpl := newTestTemplate("u2-tpl")
	u2tpl.OwnerID = strPtr("u2")
	for _, tpl := range []domain.Template{shared, u1tpl, u2tpl} {
		if err := templates.Create(ctx, tpl); err != nil {
			t.Fatalf("Create %s: %v", tpl.Name, err)
		}
	}

	list, err := templates.ListVisible(ctx, "u1", false)
	if err != nil {
		t.Fatalf("ListVisible u1: %v", err)
	}
	names := map[string]bool{}
	for _, tpl := range list {
		names[tpl.Name] = true
	}
	if !names["shared-tpl"] || !names["u1-tpl"] || names["u2-tpl"] {
		t.Fatalf("ListVisible u1 = %+v, want shared-tpl and u1-tpl only", names)
	}

	adminList, err := templates.ListVisible(ctx, "", true)
	if err != nil {
		t.Fatalf("ListVisible admin: %v", err)
	}
	if len(adminList) != 3 {
		t.Fatalf("ListVisible admin len = %d, want 3", len(adminList))
	}
}

func TestTemplates_LoopFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTemplateFixtures(t, database)
	templates := NewTemplates(database)

	tpl := newTestTemplate("until done")
	if err := templates.Create(ctx, tpl); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := templates.Get(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LoopUntil != "" || got.LoopMax != 0 {
		t.Fatalf("a template with no loop = %q/%d, want ''/0", got.LoopUntil, got.LoopMax)
	}

	tpl.LoopUntil = "done"
	tpl.LoopMax = 4
	tpl.UpdatedAt = time.Now()
	if err := templates.Update(ctx, tpl); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = templates.Get(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.LoopUntil != "done" || got.LoopMax != 4 {
		t.Fatalf("loop fields = %q/%d", got.LoopUntil, got.LoopMax)
	}

	listed, err := templates.ListVisible(ctx, "", false)
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if len(listed) != 1 || listed[0].LoopUntil != "done" || listed[0].LoopMax != 4 {
		t.Fatalf("listed = %+v", listed)
	}
}
