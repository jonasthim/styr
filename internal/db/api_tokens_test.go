package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestAPIToken(userID, name, hash string) domain.APIToken {
	return domain.APIToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		TokenHash: hash,
		Prefix:    "styr_pat_",
		CreatedAt: time.Now(),
	}
}

func TestAPITokens_CreateGetByHashTouchUsedDelete(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTestUser(t, database, "u1")
	tokens := NewAPITokens(database)

	tok := newTestAPIToken("u1", "ci", "hash-abc")
	if err := tokens.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := tokens.GetByHash(ctx, "hash-abc")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got.ID != tok.ID || got.UserID != "u1" || got.Prefix != "styr_pat_" || got.LastUsedAt != nil {
		t.Fatalf("GetByHash = %+v, want matching %+v", got, tok)
	}

	if err := tokens.TouchUsed(ctx, tok.ID, time.Now()); err != nil {
		t.Fatalf("TouchUsed: %v", err)
	}
	got, err = tokens.GetByHash(ctx, "hash-abc")
	if err != nil {
		t.Fatalf("GetByHash after TouchUsed: %v", err)
	}
	if got.LastUsedAt == nil {
		t.Fatalf("GetByHash after TouchUsed: LastUsedAt = nil, want set")
	}

	if err := tokens.Delete(ctx, tok.ID, "u1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := tokens.GetByHash(ctx, "hash-abc"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash after delete: err = %v, want ErrNotFound", err)
	}
}

func TestAPITokens_GetByHashUnknownReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	tokens := NewAPITokens(testOpenDB(t))

	if _, err := tokens.GetByHash(ctx, "no-such-hash"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByHash unknown: err = %v, want ErrNotFound", err)
	}
}

func TestAPITokens_CreateDuplicateHashConflicts(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTestUser(t, database, "u1")
	tokens := NewAPITokens(database)

	tok1 := newTestAPIToken("u1", "one", "same-hash")
	if err := tokens.Create(ctx, tok1); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	tok2 := newTestAPIToken("u1", "two", "same-hash")
	if err := tokens.Create(ctx, tok2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Create duplicate hash: err = %v, want ErrConflict", err)
	}
}

func TestAPITokens_ListByUser(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")
	tokens := NewAPITokens(database)

	if err := tokens.Create(ctx, newTestAPIToken("u1", "u1-a", "h1")); err != nil {
		t.Fatalf("Create u1-a: %v", err)
	}
	if err := tokens.Create(ctx, newTestAPIToken("u1", "u1-b", "h2")); err != nil {
		t.Fatalf("Create u1-b: %v", err)
	}
	if err := tokens.Create(ctx, newTestAPIToken("u2", "u2-a", "h3")); err != nil {
		t.Fatalf("Create u2-a: %v", err)
	}

	list, err := tokens.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListByUser len = %d, want 2", len(list))
	}
	for _, tok := range list {
		if tok.UserID != "u1" {
			t.Fatalf("ListByUser returned token for %s, want only u1", tok.UserID)
		}
	}
}

func TestAPITokens_DeleteScopedToUser(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")
	tokens := NewAPITokens(database)

	tok := newTestAPIToken("u1", "mine", "h1")
	if err := tokens.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := tokens.Delete(ctx, tok.ID, "u2"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete by wrong user: err = %v, want ErrNotFound", err)
	}
	if _, err := tokens.GetByHash(ctx, "h1"); err != nil {
		t.Fatalf("token should still exist after delete by wrong user: %v", err)
	}

	if err := tokens.Delete(ctx, tok.ID, "u1"); err != nil {
		t.Fatalf("Delete by owner: %v", err)
	}
}
