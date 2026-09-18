package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// sessionsTestFixture creates a workspace (referencing the builtin
// "interactive" profile) shared by session tests that need a valid FK.
func sessionsTestFixture(t *testing.T, ctx context.Context, d *DB) domain.Workspace {
	t.Helper()
	w := newTestWorkspace("ws-" + uuid.NewString())
	if err := NewWorkspaces(d).Create(ctx, w); err != nil {
		t.Fatalf("create workspace fixture: %v", err)
	}
	return w
}

// createTestUserID inserts a user row with a specific id (sessions.owner_user_id
// is a foreign key, so tests that set an owner must create the user first)
// and returns a pointer to that id.
func createTestUserID(t *testing.T, ctx context.Context, d *DB, id string) *string {
	t.Helper()
	u := newTestUser("https://issuer", id)
	u.ID = id
	if err := NewUsers(d).Create(ctx, u); err != nil {
		t.Fatalf("create user fixture %q: %v", id, err)
	}
	return &id
}

func newTestSession(workspaceID string, ownerID *string, state domain.SessionState) domain.Session {
	now := time.Now()
	return domain.Session{
		ID:           uuid.NewString(),
		OwnerID:      ownerID,
		Title:        "session",
		WorkspaceID:  workspaceID,
		ProfileID:    "interactive",
		Harness:      "claude",
		State:        state,
		Origin:       domain.OriginUI,
		CreatedAt:    now,
		LastActiveAt: now,
	}
}

func strPtr(s string) *string { return &s }

func TestSessions_CreateGetUpdates(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := sessionsTestFixture(t, ctx, d)
	sessions := NewSessions(d)

	s := newTestSession(ws.ID, createTestUserID(t, ctx, d, "user-1"), domain.SessionOpen)
	if err := sessions.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := sessions.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != domain.SessionOpen || got.OwnerID == nil || *got.OwnerID != "user-1" {
		t.Fatalf("Get = %+v", got)
	}

	if err := sessions.UpdateState(ctx, s.ID, domain.SessionRunning); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if err := sessions.UpdateStats(ctx, s.ID, 5, 1.23, 100, 200); err != nil {
		t.Fatalf("UpdateStats: %v", err)
	}
	if err := sessions.UpdateNow(ctx, s.ID, "running tests"); err != nil {
		t.Fatalf("UpdateNow: %v", err)
	}
	if err := sessions.UpdateModel(ctx, s.ID, "claude-sonnet"); err != nil {
		t.Fatalf("UpdateModel: %v", err)
	}
	if err := sessions.Touch(ctx, s.ID); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err = sessions.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get after updates: %v", err)
	}
	if got.State != domain.SessionRunning || got.NumTurns != 5 || got.CostUSD != 1.23 ||
		got.TokensIn != 100 || got.TokensOut != 200 || got.NowLine != "running tests" || got.Model != "claude-sonnet" {
		t.Fatalf("Get after updates = %+v", got)
	}
}

func TestSessions_UpdateStateNotFound(t *testing.T) {
	ctx := context.Background()
	sessions := NewSessions(testOpenDB(t))
	if err := sessions.UpdateState(ctx, "missing", domain.SessionClosed); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateState: err = %v, want ErrNotFound", err)
	}
}

func TestSessions_ListVisible(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := sessionsTestFixture(t, ctx, d)
	sessions := NewSessions(d)

	mine := newTestSession(ws.ID, createTestUserID(t, ctx, d, "user-1"), domain.SessionOpen)
	other := newTestSession(ws.ID, createTestUserID(t, ctx, d, "user-2"), domain.SessionOpen)
	unowned := newTestSession(ws.ID, nil, domain.SessionOpen)
	for _, s := range []domain.Session{mine, other, unowned} {
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatalf("Create %q: %v", s.ID, err)
		}
	}

	visible, err := sessions.ListVisible(ctx, "user-1", false)
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	ids := map[string]bool{}
	for _, s := range visible {
		ids[s.ID] = true
	}
	if !ids[mine.ID] {
		t.Fatalf("ListVisible missing own session")
	}
	if !ids[unowned.ID] {
		t.Fatalf("ListVisible missing owner-null session")
	}
	if ids[other.ID] {
		t.Fatalf("ListVisible leaked another member's session")
	}
	if len(visible) != 2 {
		t.Fatalf("ListVisible len = %d, want 2", len(visible))
	}

	adminVisible, err := sessions.ListVisible(ctx, "user-1", true)
	if err != nil {
		t.Fatalf("ListVisible admin: %v", err)
	}
	if len(adminVisible) != 3 {
		t.Fatalf("ListVisible admin len = %d, want 3", len(adminVisible))
	}
}

func TestSessions_ListVisibleOrdering(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := sessionsTestFixture(t, ctx, d)
	sessions := NewSessions(d)

	// Create in an order that would sort wrong if state priority were ignored.
	closed := newTestSession(ws.ID, nil, domain.SessionClosed)
	failed := newTestSession(ws.ID, nil, domain.SessionFailed)
	waiting := newTestSession(ws.ID, nil, domain.SessionWaiting)
	running := newTestSession(ws.ID, nil, domain.SessionRunning)
	open := newTestSession(ws.ID, nil, domain.SessionOpen)
	for _, s := range []domain.Session{closed, failed, waiting, running, open} {
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatalf("Create %q: %v", s.ID, err)
		}
	}

	visible, err := sessions.ListVisible(ctx, "user-1", true)
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if len(visible) != 5 {
		t.Fatalf("ListVisible len = %d, want 5", len(visible))
	}
	wantOrder := []string{waiting.ID, running.ID, open.ID, closed.ID, failed.ID}
	for i, s := range visible {
		if s.ID != wantOrder[i] {
			t.Fatalf("ListVisible[%d] = %q (state %s), want %q", i, s.ID, s.State, wantOrder[i])
		}
	}
}

func TestSessions_EffortAndSlashCommandsRoundTrip(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := sessionsTestFixture(t, ctx, d)
	sessions := NewSessions(d)

	sess := newTestSession(ws.ID, nil, domain.SessionRunning)
	sess.Effort = "medium"
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := sessions.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Effort != "medium" {
		t.Errorf("effort = %q, want medium", got.Effort)
	}
	if len(got.SlashCommands) != 0 {
		t.Errorf("slash commands = %v, want empty before the first init", got.SlashCommands)
	}

	if err := sessions.UpdateEffort(ctx, sess.ID, "high"); err != nil {
		t.Fatalf("UpdateEffort: %v", err)
	}
	if err := sessions.UpdateSlashCommands(ctx, sess.ID, []string{"compact", "superpowers:brainstorming"}); err != nil {
		t.Fatalf("UpdateSlashCommands: %v", err)
	}
	got, err = sessions.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get after updates: %v", err)
	}
	if got.Effort != "high" {
		t.Errorf("effort = %q, want high", got.Effort)
	}
	if len(got.SlashCommands) != 2 || got.SlashCommands[0] != "compact" || got.SlashCommands[1] != "superpowers:brainstorming" {
		t.Errorf("slash commands = %v, want [compact superpowers:brainstorming]", got.SlashCommands)
	}
}

func TestSessions_UpdateEffortNotFound(t *testing.T) {
	sessions := NewSessions(testOpenDB(t))
	if err := sessions.UpdateEffort(context.Background(), "missing", "low"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateEffort on a missing session = %v, want ErrNotFound", err)
	}
}
