package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

func TestLoginSessions_CreateGetByHash(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	users := NewUsers(d)
	sessions := NewLoginSessions(d)

	u := newTestUser("https://issuer", "sub-1")
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	expires := time.Now().Add(time.Hour)
	id, err := sessions.Create(ctx, u.ID, "hash-1", expires, "test-agent")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatalf("Create returned empty id")
	}

	userID, gotExpires, err := sessions.GetByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if userID != u.ID {
		t.Fatalf("userID = %q, want %q", userID, u.ID)
	}
	if gotExpires.Unix() != expires.Unix() {
		t.Fatalf("expires = %v, want %v", gotExpires, expires)
	}
}

func TestLoginSessions_GetByHashUnknownNotFound(t *testing.T) {
	ctx := context.Background()
	sessions := NewLoginSessions(testOpenDB(t))

	_, _, err := sessions.GetByHash(ctx, "no-such-hash")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash: err = %v, want ErrNotFound", err)
	}
}

func TestLoginSessions_TouchDeleteDeleteAllForUser(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	users := NewUsers(d)
	sessions := NewLoginSessions(d)

	u := newTestUser("https://issuer", "sub-2")
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	id1, err := sessions.Create(ctx, u.ID, "hash-a", time.Now().Add(time.Hour), "ua")
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	if _, err := sessions.Create(ctx, u.ID, "hash-b", time.Now().Add(time.Hour), "ua"); err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	if err := sessions.Touch(ctx, id1); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	if err := sessions.DeleteAllForUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	if _, _, err := sessions.GetByHash(ctx, "hash-a"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash after DeleteAllForUser: err = %v, want ErrNotFound", err)
	}
	if _, _, err := sessions.GetByHash(ctx, "hash-b"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash after DeleteAllForUser: err = %v, want ErrNotFound", err)
	}
}

func TestLoginSessions_Delete(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	users := NewUsers(d)
	sessions := NewLoginSessions(d)

	u := newTestUser("https://issuer", "sub-3")
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	id, err := sessions.Create(ctx, u.ID, "hash-c", time.Now().Add(time.Hour), "ua")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := sessions.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := sessions.GetByHash(ctx, "hash-c"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash after Delete: err = %v, want ErrNotFound", err)
	}
}
