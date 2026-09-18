package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestProfile(name string) domain.Profile {
	return domain.Profile{
		ID:              uuid.NewString(),
		Name:            name,
		Mode:            "default",
		AllowedTools:    []string{"Read", "Grep"},
		DisallowedTools: []string{"WebFetch"},
		MaxTurns:        10,
		Unattended:      true,
		ApprovalTimeout: 30 * time.Minute,
		Builtin:         false,
	}
}

func TestProfiles_BuiltinRowsSeeded(t *testing.T) {
	ctx := context.Background()
	profiles := NewProfiles(testOpenDB(t))

	list, err := profiles.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3 builtin profiles", len(list))
	}

	interactive, err := profiles.Get(ctx, "interactive")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !interactive.Builtin {
		t.Fatalf("interactive.Builtin = false, want true")
	}
}

func TestProfiles_CreateGetUpdate(t *testing.T) {
	ctx := context.Background()
	profiles := NewProfiles(testOpenDB(t))

	p := newTestProfile("custom")
	if err := profiles.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := profiles.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != p.Name || got.MaxTurns != p.MaxTurns || !got.Unattended {
		t.Fatalf("Get = %+v, want matching %+v", got, p)
	}
	if len(got.AllowedTools) != 2 || got.AllowedTools[0] != "Read" {
		t.Fatalf("AllowedTools = %v", got.AllowedTools)
	}
	if got.ApprovalTimeout != 30*time.Minute {
		t.Fatalf("ApprovalTimeout = %v, want 30m", got.ApprovalTimeout)
	}

	p.MaxTurns = 20
	p.Unattended = false
	if err := profiles.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = profiles.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.MaxTurns != 20 || got.Unattended {
		t.Fatalf("Get after update = %+v", got)
	}
}

func TestProfiles_CreateDuplicateNameConflicts(t *testing.T) {
	ctx := context.Background()
	profiles := NewProfiles(testOpenDB(t))

	p := newTestProfile("dup-profile")
	if err := profiles.Create(ctx, p); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	p2 := newTestProfile("dup-profile")
	if err := profiles.Create(ctx, p2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate: err = %v, want ErrConflict", err)
	}
}

func TestProfiles_ModelAndEffortSeededOnUnattendedBuiltins(t *testing.T) {
	ctx := context.Background()
	profiles := NewProfiles(testOpenDB(t))

	for _, id := range []string{"investigate", "remediate"} {
		p, err := profiles.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if p.Model != "sonnet" || p.Effort != "medium" {
			t.Errorf("%s: model/effort = %q/%q, want sonnet/medium", id, p.Model, p.Effort)
		}
	}
	interactive, err := profiles.Get(ctx, "interactive")
	if err != nil {
		t.Fatalf("Get interactive: %v", err)
	}
	if interactive.Model != "" || interactive.Effort != "" {
		t.Errorf("interactive: model/effort = %q/%q, want the CLI defaults (empty)", interactive.Model, interactive.Effort)
	}
}

func TestProfiles_UpdateModelAndEffort(t *testing.T) {
	ctx := context.Background()
	profiles := NewProfiles(testOpenDB(t))

	p := newTestProfile("with-model")
	p.Model, p.Effort = "opus", "high"
	if err := profiles.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := profiles.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Model != "opus" || got.Effort != "high" {
		t.Fatalf("after Create: model/effort = %q/%q, want opus/high", got.Model, got.Effort)
	}

	got.Model, got.Effort = "haiku", "low"
	if err := profiles.Update(ctx, *got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = profiles.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Model != "haiku" || got.Effort != "low" {
		t.Fatalf("after Update: model/effort = %q/%q, want haiku/low", got.Model, got.Effort)
	}
}
