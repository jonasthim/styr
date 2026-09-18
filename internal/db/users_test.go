package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestUser(issuer, subject string) domain.User {
	now := time.Now()
	return domain.User{
		ID:          uuid.NewString(),
		Issuer:      issuer,
		Subject:     subject,
		Email:       "a@example.com",
		DisplayName: "Alice",
		Role:        domain.RoleMember,
		CreatedAt:   now,
		LastLoginAt: now,
	}
}

func TestUsers_CreateGet(t *testing.T) {
	ctx := context.Background()
	users := NewUsers(testOpenDB(t))

	u := newTestUser("https://issuer", "sub-1")
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := users.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Email != u.Email || got.Subject != u.Subject {
		t.Fatalf("GetByID = %+v, want matching %+v", got, u)
	}

	got, err = users.GetBySubject(ctx, u.Issuer, u.Subject)
	if err != nil {
		t.Fatalf("GetBySubject: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("GetBySubject id = %q, want %q", got.ID, u.ID)
	}
}

func TestUsers_CreateDuplicateIssuerSubjectConflicts(t *testing.T) {
	ctx := context.Background()
	users := NewUsers(testOpenDB(t))

	u1 := newTestUser("https://issuer", "sub-dup")
	if err := users.Create(ctx, u1); err != nil {
		t.Fatalf("Create first: %v", err)
	}

	u2 := newTestUser("https://issuer", "sub-dup")
	err := users.Create(ctx, u2)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate: err = %v, want ErrConflict", err)
	}
}

func TestUsers_GetByIDNotFound(t *testing.T) {
	ctx := context.Background()
	users := NewUsers(testOpenDB(t))

	_, err := users.GetByID(ctx, "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID: err = %v, want ErrNotFound", err)
	}
}

func TestUsers_ListAndCount(t *testing.T) {
	ctx := context.Background()
	users := NewUsers(testOpenDB(t))

	for i := 0; i < 3; i++ {
		u := newTestUser("https://issuer", uuid.NewString())
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	list, err := users.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}

	n, err := users.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Fatalf("Count = %d, want 3", n)
	}
}

func TestUsers_UpdateRoleTouchLoginUpdatePrefs(t *testing.T) {
	ctx := context.Background()
	users := NewUsers(testOpenDB(t))

	u := newTestUser("https://issuer", "sub-update")
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := users.UpdateRole(ctx, u.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if err := users.TouchLogin(ctx, u.ID); err != nil {
		t.Fatalf("TouchLogin: %v", err)
	}
	prefs := json.RawMessage(`{"theme":"dark"}`)
	if err := users.UpdatePrefs(ctx, u.ID, prefs); err != nil {
		t.Fatalf("UpdatePrefs: %v", err)
	}

	got, err := users.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Role != domain.RoleAdmin {
		t.Fatalf("Role = %q, want admin", got.Role)
	}
	if !got.LastLoginAt.After(u.LastLoginAt.Add(-time.Second)) {
		t.Fatalf("LastLoginAt not updated: %v", got.LastLoginAt)
	}
	if string(got.Prefs) != string(prefs) {
		t.Fatalf("Prefs = %s, want %s", got.Prefs, prefs)
	}
}
