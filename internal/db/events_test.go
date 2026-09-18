package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
)

// eventsTestFixture creates a workspace and session so events can reference
// a valid session_id.
func eventsTestFixture(t *testing.T, ctx context.Context, d *DB) domain.Session {
	t.Helper()
	ws := sessionsTestFixture(t, ctx, d)
	s := newTestSession(ws.ID, nil, domain.SessionOpen)
	if err := NewSessions(d).Create(ctx, s); err != nil {
		t.Fatalf("create session fixture: %v", err)
	}
	return s
}

func TestEvents_AppendSequencesAndListAfter(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	s := eventsTestFixture(t, ctx, d)
	events := NewEvents(d)

	payload := json.RawMessage(`{"n":1}`)
	seq1, err := events.Append(ctx, s.ID, "message", payload)
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	seq2, err := events.Append(ctx, s.ID, "message", payload)
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	seq3, err := events.Append(ctx, s.ID, "message", payload)
	if err != nil {
		t.Fatalf("Append 3: %v", err)
	}
	if seq1 != 1 || seq2 != 2 || seq3 != 3 {
		t.Fatalf("seqs = %d, %d, %d, want 1, 2, 3", seq1, seq2, seq3)
	}

	after, err := events.ListAfter(ctx, s.ID, 1, 10)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("ListAfter len = %d, want 2", len(after))
	}
	if after[0].Seq != 2 || after[1].Seq != 3 {
		t.Fatalf("ListAfter seqs = %d, %d, want 2, 3", after[0].Seq, after[1].Seq)
	}
}

func TestEvents_AppendPerSessionIndependentSequences(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := sessionsTestFixture(t, ctx, d)
	sessions := NewSessions(d)
	events := NewEvents(d)

	s1 := newTestSession(ws.ID, nil, domain.SessionOpen)
	s2 := newTestSession(ws.ID, nil, domain.SessionOpen)
	if err := sessions.Create(ctx, s1); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	if err := sessions.Create(ctx, s2); err != nil {
		t.Fatalf("create s2: %v", err)
	}

	seq, err := events.Append(ctx, s1.ID, "message", json.RawMessage(`{}`))
	if err != nil || seq != 1 {
		t.Fatalf("Append s1: seq=%d err=%v", seq, err)
	}
	seq, err = events.Append(ctx, s2.ID, "message", json.RawMessage(`{}`))
	if err != nil || seq != 1 {
		t.Fatalf("Append s2: seq=%d err=%v, want seq 1", seq, err)
	}
}

func TestEvents_ListAfterLimit(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	s := eventsTestFixture(t, ctx, d)
	events := NewEvents(d)

	for i := 0; i < 5; i++ {
		if _, err := events.Append(ctx, s.ID, "message", json.RawMessage(`{}`)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	limited, err := events.ListAfter(ctx, s.ID, 0, 2)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("ListAfter len = %d, want 2", len(limited))
	}
}
