package db

import (
	"context"
	"testing"
)

func TestAudit_AppendAndList(t *testing.T) {
	ctx := context.Background()
	audit := NewAudit(testOpenDB(t))

	if err := audit.Append(ctx, "user-1", "session.create", "session-1", map[string]any{"title": "demo"}); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if err := audit.Append(ctx, "user-1", "session.close", "session-1", nil); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	list, err := audit.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	// Newest first.
	if list[0].Action != "session.close" || list[1].Action != "session.create" {
		t.Fatalf("List order = %q, %q", list[0].Action, list[1].Action)
	}
	if string(list[1].Detail) != `{"title":"demo"}` {
		t.Fatalf("Detail = %s", list[1].Detail)
	}
	if string(list[0].Detail) != `{}` {
		t.Fatalf("Detail for nil = %s, want {}", list[0].Detail)
	}
}

func TestAudit_ListRespectsLimit(t *testing.T) {
	ctx := context.Background()
	audit := NewAudit(testOpenDB(t))

	for i := 0; i < 5; i++ {
		if err := audit.Append(ctx, "user-1", "action", "target", nil); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	list, err := audit.List(ctx, 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
}
