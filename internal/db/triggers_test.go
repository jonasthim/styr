package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// seedTriggerFixtures creates the workspace/profile/template rows a
// trigger's template_id foreign key needs, returning the template id.
func seedTriggerFixtures(t *testing.T, d *DB) string {
	t.Helper()
	seedTemplateFixtures(t, d)
	tpl := newTestTemplate("base-template")
	if err := NewTemplates(d).Create(context.Background(), tpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	return tpl.ID
}

func newTestTrigger(templateID, name, slug string) domain.Trigger {
	now := time.Now()
	return domain.Trigger{
		ID:              uuid.NewString(),
		Name:            name,
		Slug:            slug,
		Kind:            domain.TriggerGrafana,
		SecretHash:      "hash",
		SecretHint:      "…ab12",
		TemplateID:      templateID,
		Enabled:         true,
		CooldownS:       600,
		StormCapPerHour: 10,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func TestTriggers_CreateGetGetBySlugUpdateDelete(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedTriggerFixtures(t, database)
	triggers := NewTriggers(database)

	tr := newTestTrigger(tplID, "grafana alerts", "grafana-alerts")
	if err := triggers.Create(ctx, tr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := triggers.Get(ctx, tr.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != tr.Slug || got.Kind != domain.TriggerGrafana || got.SecretHash != "hash" || got.LastDeliveryAt != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, tr)
	}

	bySlug, err := triggers.GetBySlug(ctx, "grafana-alerts")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if bySlug.ID != tr.ID {
		t.Fatalf("GetBySlug id = %s, want %s", bySlug.ID, tr.ID)
	}
	if _, err := triggers.GetBySlug(ctx, "no-such-slug"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetBySlug unknown: err = %v, want ErrNotFound", err)
	}

	tr.Name = "renamed"
	tr.Enabled = false
	tr.UpdatedAt = time.Now()
	if err := triggers.Update(ctx, tr); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = triggers.Get(ctx, tr.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Name != "renamed" || got.Enabled {
		t.Fatalf("Get after update = %+v", got)
	}
	// Update must not clobber the secret set outside it.
	if got.SecretHash != "hash" {
		t.Fatalf("Update clobbered secret hash: got %q", got.SecretHash)
	}

	if err := triggers.Delete(ctx, tr.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := triggers.Get(ctx, tr.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestTriggers_CreateDuplicateSlugConflicts(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedTriggerFixtures(t, database)
	triggers := NewTriggers(database)

	tr1 := newTestTrigger(tplID, "one", "dup-slug")
	if err := triggers.Create(ctx, tr1); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	tr2 := newTestTrigger(tplID, "two", "dup-slug")
	if err := triggers.Create(ctx, tr2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate slug: err = %v, want ErrConflict", err)
	}
}

func TestTriggers_ListVisible(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedTriggerFixtures(t, database)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")
	triggers := NewTriggers(database)

	shared := newTestTrigger(tplID, "shared", "shared-slug")
	u1tr := newTestTrigger(tplID, "u1-trigger", "u1-slug")
	u1tr.OwnerID = strPtr("u1")
	u2tr := newTestTrigger(tplID, "u2-trigger", "u2-slug")
	u2tr.OwnerID = strPtr("u2")
	for _, tr := range []domain.Trigger{shared, u1tr, u2tr} {
		if err := triggers.Create(ctx, tr); err != nil {
			t.Fatalf("Create %s: %v", tr.Name, err)
		}
	}

	list, err := triggers.ListVisible(ctx, "u1", false)
	if err != nil {
		t.Fatalf("ListVisible u1: %v", err)
	}
	names := map[string]bool{}
	for _, tr := range list {
		names[tr.Name] = true
	}
	if !names["shared"] || !names["u1-trigger"] || names["u2-trigger"] {
		t.Fatalf("ListVisible u1 = %+v, want shared and u1-trigger only", names)
	}

	adminList, err := triggers.ListVisible(ctx, "", true)
	if err != nil {
		t.Fatalf("ListVisible admin: %v", err)
	}
	if len(adminList) != 3 {
		t.Fatalf("ListVisible admin len = %d, want 3", len(adminList))
	}
}

func TestTriggers_SetSecretAndTouchDelivery(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedTriggerFixtures(t, database)
	triggers := NewTriggers(database)

	tr := newTestTrigger(tplID, "rotate", "rotate-slug")
	if err := triggers.Create(ctx, tr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := triggers.SetSecret(ctx, tr.ID, "new-hash", "…cd34"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	got, err := triggers.Get(ctx, tr.ID)
	if err != nil {
		t.Fatalf("Get after SetSecret: %v", err)
	}
	if got.SecretHash != "new-hash" || got.SecretHint != "…cd34" {
		t.Fatalf("Get after SetSecret = %+v", got)
	}

	at := time.Now().Truncate(time.Second)
	if err := triggers.TouchDelivery(ctx, tr.ID, at); err != nil {
		t.Fatalf("TouchDelivery: %v", err)
	}
	got, err = triggers.Get(ctx, tr.ID)
	if err != nil {
		t.Fatalf("Get after TouchDelivery: %v", err)
	}
	if got.LastDeliveryAt == nil || !got.LastDeliveryAt.Equal(at.UTC()) {
		t.Fatalf("LastDeliveryAt = %v, want %v", got.LastDeliveryAt, at)
	}

	if err := triggers.SetSecret(ctx, "no-such-id", "h", "hint"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetSecret unknown id: err = %v, want ErrNotFound", err)
	}
	if err := triggers.TouchDelivery(ctx, "no-such-id", at); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("TouchDelivery unknown id: err = %v, want ErrNotFound", err)
	}
}
