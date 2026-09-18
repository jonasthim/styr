package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestApproval(sessionID string, createdAt time.Time) domain.Approval {
	return domain.Approval{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		RequestID: "req-" + uuid.NewString(),
		Tool:      "Bash",
		Input:     json.RawMessage(`{"command":"ls"}`),
		Risk:      domain.RiskRead,
		State:     domain.ApprovalPending,
		CreatedAt: createdAt,
	}
}

func TestApprovals_CreateGetDecide(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	s := eventsTestFixture(t, ctx, d)
	approvals := NewApprovals(d)

	a := newTestApproval(s.ID, time.Now())
	if err := approvals.Create(ctx, a); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := approvals.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != domain.ApprovalPending || got.Tool != "Bash" {
		t.Fatalf("Get = %+v", got)
	}

	updated := json.RawMessage(`{"command":"ls -la"}`)
	if err := approvals.Decide(ctx, a.ID, domain.ApprovalAllowed, "user-1", updated, "looks fine"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	got, err = approvals.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get after decide: %v", err)
	}
	if got.State != domain.ApprovalAllowed || got.DecidedBy == nil || *got.DecidedBy != "user-1" ||
		got.DecidedAt == nil || string(got.UpdatedInput) != string(updated) || got.Message != "looks fine" {
		t.Fatalf("Get after decide = %+v", got)
	}
}

func TestApprovals_Snooze(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	s := eventsTestFixture(t, ctx, d)
	approvals := NewApprovals(d)

	a := newTestApproval(s.ID, time.Now())
	if err := approvals.Create(ctx, a); err != nil {
		t.Fatalf("Create: %v", err)
	}
	until := time.Now().Add(time.Hour)
	if err := approvals.Snooze(ctx, a.ID, until); err != nil {
		t.Fatalf("Snooze: %v", err)
	}
	got, err := approvals.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SnoozedUntil == nil || got.SnoozedUntil.Unix() != until.Unix() {
		t.Fatalf("SnoozedUntil = %v, want %v", got.SnoozedUntil, until)
	}
}

func TestApprovals_ExpireBeforeFlipsOnlyOldPending(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	s := eventsTestFixture(t, ctx, d)
	approvals := NewApprovals(d)

	cutoff := time.Now()
	old := newTestApproval(s.ID, cutoff.Add(-time.Hour))
	recent := newTestApproval(s.ID, cutoff.Add(time.Hour))
	alreadyDecided := newTestApproval(s.ID, cutoff.Add(-time.Hour))
	alreadyDecided.State = domain.ApprovalAllowed

	for _, a := range []domain.Approval{old, recent, alreadyDecided} {
		if err := approvals.Create(ctx, a); err != nil {
			t.Fatalf("Create %q: %v", a.ID, err)
		}
	}

	n, err := approvals.ExpireBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("ExpireBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("ExpireBefore n = %d, want 1", n)
	}

	gotOld, err := approvals.Get(ctx, old.ID)
	if err != nil {
		t.Fatalf("Get old: %v", err)
	}
	if gotOld.State != domain.ApprovalExpired {
		t.Fatalf("old.State = %q, want expired", gotOld.State)
	}

	gotRecent, err := approvals.Get(ctx, recent.ID)
	if err != nil {
		t.Fatalf("Get recent: %v", err)
	}
	if gotRecent.State != domain.ApprovalPending {
		t.Fatalf("recent.State = %q, want pending", gotRecent.State)
	}

	gotDecided, err := approvals.Get(ctx, alreadyDecided.ID)
	if err != nil {
		t.Fatalf("Get alreadyDecided: %v", err)
	}
	if gotDecided.State != domain.ApprovalAllowed {
		t.Fatalf("alreadyDecided.State = %q, want allowed (unaffected)", gotDecided.State)
	}
}

func TestApprovals_ListPendingVisibleAndPendingForSession(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := sessionsTestFixture(t, ctx, d)
	sessions := NewSessions(d)
	approvals := NewApprovals(d)

	mineSession := newTestSession(ws.ID, createTestUserID(t, ctx, d, "user-1"), domain.SessionWaiting)
	otherSession := newTestSession(ws.ID, createTestUserID(t, ctx, d, "user-2"), domain.SessionWaiting)
	unownedSession := newTestSession(ws.ID, nil, domain.SessionWaiting)
	for _, s := range []domain.Session{mineSession, otherSession, unownedSession} {
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}

	mineApproval := newTestApproval(mineSession.ID, time.Now())
	otherApproval := newTestApproval(otherSession.ID, time.Now())
	unownedApproval := newTestApproval(unownedSession.ID, time.Now())
	for _, a := range []domain.Approval{mineApproval, otherApproval, unownedApproval} {
		if err := approvals.Create(ctx, a); err != nil {
			t.Fatalf("create approval: %v", err)
		}
	}

	visible, err := approvals.ListPendingVisible(ctx, "user-1", false)
	if err != nil {
		t.Fatalf("ListPendingVisible: %v", err)
	}
	ids := map[string]bool{}
	for _, a := range visible {
		ids[a.ID] = true
	}
	if !ids[mineApproval.ID] || !ids[unownedApproval.ID] || ids[otherApproval.ID] {
		t.Fatalf("ListPendingVisible visibility wrong: %+v", visible)
	}

	adminVisible, err := approvals.ListPendingVisible(ctx, "user-1", true)
	if err != nil {
		t.Fatalf("ListPendingVisible admin: %v", err)
	}
	if len(adminVisible) != 3 {
		t.Fatalf("ListPendingVisible admin len = %d, want 3", len(adminVisible))
	}

	forSession, err := approvals.PendingForSession(ctx, mineSession.ID)
	if err != nil {
		t.Fatalf("PendingForSession: %v", err)
	}
	if len(forSession) != 1 || forSession[0].ID != mineApproval.ID {
		t.Fatalf("PendingForSession = %+v", forSession)
	}
}
