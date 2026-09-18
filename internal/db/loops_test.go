package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// seedLoopFixtures creates the workspace, template and session a loop's
// foreign keys need, returning the template and session ids.
func seedLoopFixtures(t *testing.T, d *DB) (templateID, sessionID string) {
	t.Helper()
	ctx := context.Background()
	sessionID = seedRunFixtures(t, d)
	tpl := newTestTemplate("looping template")
	tpl.LoopUntil = "done"
	tpl.LoopMax = 3
	if err := NewTemplates(d).Create(ctx, tpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	return tpl.ID, sessionID
}

func TestLoops_CreateGetUpdate(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	templateID, sessionID := seedLoopFixtures(t, database)
	loops := NewLoops(database)

	now := time.Now()
	l := domain.Loop{
		ID: uuid.NewString(), TemplateID: templateID, SessionID: &sessionID,
		Origin: string(domain.OriginWebhook), OriginRef: "trg-1", UntilField: "done",
		MaxIterations: 3, Iteration: 1, State: domain.LoopRunning, CreatedAt: now, UpdatedAt: now,
	}
	if err := loops.Create(ctx, l); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := loops.Get(ctx, l.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TemplateID != templateID || got.SessionID == nil || *got.SessionID != sessionID {
		t.Fatalf("Get = %+v", got)
	}
	if got.UntilField != "done" || got.MaxIterations != 3 || got.Iteration != 1 {
		t.Fatalf("Get = %+v", got)
	}
	if got.State != domain.LoopRunning || got.Origin != string(domain.OriginWebhook) || got.OriginRef != "trg-1" {
		t.Fatalf("Get = %+v", got)
	}

	if err := loops.Update(ctx, l.ID, domain.LoopExhausted, 3, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = loops.Get(ctx, l.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.State != domain.LoopExhausted || got.Iteration != 3 {
		t.Fatalf("after update = %+v", got)
	}
	// A nil session id leaves the stored one alone.
	if got.SessionID == nil || *got.SessionID != sessionID {
		t.Fatalf("session id = %v, want it untouched", got.SessionID)
	}
	if !got.UpdatedAt.After(got.CreatedAt) && got.UpdatedAt.Before(got.CreatedAt) {
		t.Fatalf("updated_at = %v, created_at = %v", got.UpdatedAt, got.CreatedAt)
	}

	if err := loops.Update(ctx, "nope", domain.LoopDone, 1, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update unknown: err = %v, want ErrNotFound", err)
	}
	if _, err := loops.Get(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get unknown: err = %v, want ErrNotFound", err)
	}
}

func TestLoops_CreateWithoutSessionThenAttachOne(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	templateID, sessionID := seedLoopFixtures(t, database)
	loops := NewLoops(database)

	now := time.Now()
	l := domain.Loop{ID: uuid.NewString(), TemplateID: templateID, Origin: "ui", UntilField: "done",
		MaxIterations: 2, State: domain.LoopRunning, CreatedAt: now, UpdatedAt: now}
	if err := loops.Create(ctx, l); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := loops.Get(ctx, l.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SessionID != nil {
		t.Fatalf("session id = %v, want nil", *got.SessionID)
	}

	if err := loops.Update(ctx, l.ID, domain.LoopRunning, 1, &sessionID); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = loops.Get(ctx, l.ID)
	if err != nil {
		t.Fatalf("Get after attach: %v", err)
	}
	if got.SessionID == nil || *got.SessionID != sessionID {
		t.Fatalf("session id = %v, want %q", got.SessionID, sessionID)
	}
}

func TestLoops_ListFiltersByStateAndLimits(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	templateID, _ := seedLoopFixtures(t, database)
	loops := NewLoops(database)

	base := time.Now()
	states := []domain.LoopState{domain.LoopRunning, domain.LoopRunning, domain.LoopDone}
	for i, st := range states {
		at := base.Add(time.Duration(i) * time.Second)
		if err := loops.Create(ctx, domain.Loop{
			ID: uuid.NewString(), TemplateID: templateID, Origin: "webhook", UntilField: "done",
			MaxIterations: 2, State: st, CreatedAt: at, UpdatedAt: at,
		}); err != nil {
			t.Fatalf("create loop %d: %v", i, err)
		}
	}

	all, err := loops.List(ctx, "", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all = %d loops, want 3", len(all))
	}
	// Newest first.
	if all[0].State != domain.LoopDone {
		t.Fatalf("first = %+v, want the newest (done) loop", all[0])
	}

	running, err := loops.List(ctx, string(domain.LoopRunning), 0)
	if err != nil {
		t.Fatalf("List running: %v", err)
	}
	if len(running) != 2 {
		t.Fatalf("running = %d loops, want 2", len(running))
	}

	limited, err := loops.List(ctx, "", 1)
	if err != nil {
		t.Fatalf("List limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limited = %d loops, want 1", len(limited))
	}

	none, err := loops.List(ctx, string(domain.LoopStopped), 0)
	if err != nil {
		t.Fatalf("List stopped: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("stopped = %+v, want empty", none)
	}
}

func TestLoops_RejectsAnUnknownState(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	templateID, _ := seedLoopFixtures(t, database)
	loops := NewLoops(database)

	now := time.Now()
	err := loops.Create(ctx, domain.Loop{ID: uuid.NewString(), TemplateID: templateID, Origin: "ui",
		UntilField: "done", MaxIterations: 1, State: "spinning", CreatedAt: now, UpdatedAt: now})
	if err == nil {
		t.Fatal("want the state CHECK constraint to reject an unknown state")
	}
}
