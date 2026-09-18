package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

// Finding 4 regression test: deciding an approval a second time must not
// silently re-decide it or send a second control_response to the child.
// Approvals.Decide's "AND state = 'pending'" guard (internal/db/approvals.go)
// makes Service.Decide's database write the sole arbiter of who wins;
// Service.Decide performs that write before ever touching the child, so
// the loser sees domain.ErrConflict and returns without calling
// proc.Decide again.
func TestDecide_Twice_SecondReturnsConflictAndProcessSeesOneDecision(t *testing.T) {
	step := fake.Step{
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{RequestID: "req-1", ToolName: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)}},
			{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
		},
		WaitForDecision: true,
	}
	svc, repos, h := newService(t, step)
	actor := Actor{UserID: testAdminID, IsAdmin: true}
	owner := testAdminID

	sess, err := svc.Create(context.Background(), actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionWaiting)

	pending, err := repos.Approvals.PendingForSession(context.Background(), sess.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending approvals = %+v, err = %v", pending, err)
	}
	ap := pending[0]

	if err := svc.Decide(context.Background(), actor, ap.ID, true, nil, "first"); err != nil {
		t.Fatalf("first decide: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	err = svc.Decide(context.Background(), actor, ap.ID, false, nil, "second")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second decide error = %v, want ErrConflict", err)
	}

	if len(h.Procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(h.Procs))
	}
	if n := len(h.Procs[0].Decisions); n != 1 {
		t.Fatalf("process saw %d decisions, want exactly 1: %+v", n, h.Procs[0].Decisions)
	}
	if !h.Procs[0].Decisions[0].Allow {
		t.Errorf("recorded decision = deny, want the first (allow) decision to have stuck")
	}

	got, err := repos.Approvals.Get(context.Background(), ap.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if got.State != domain.ApprovalAllowed || got.Message != "first" {
		t.Fatalf("approval = %+v, want the first decision to have stuck", got)
	}
}

// Finding 4 regression test: once a human has decided an approval,
// RunMaintenance's expiry sweep must not decide it again, even if the
// approval's age would otherwise make it eligible for expiry. Since
// ListPendingVisible only returns approvals still in state 'pending', an
// approval Service.Decide already resolved is simply absent from
// expireApprovals' worklist — it never reaches the point of calling
// proc.Decide a second time.
func TestExpireApprovals_AfterHumanDecide_NoSecondDecision(t *testing.T) {
	step := fake.Step{
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{RequestID: "req-1", ToolName: "Bash", Input: json.RawMessage(`{"command":"rm -rf /tmp/x"}`)}},
			{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
		},
		WaitForDecision: true,
	}
	svc, repos, h := newService(t, step)

	profile := domain.Profile{ID: "test-unattended-decide-then-expire", Name: "test-unattended-decide-then-expire", Mode: "auto", Unattended: true, ApprovalTimeout: 20 * time.Millisecond}
	if err := repos.Profiles.Create(context.Background(), profile); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	actor := Actor{UserID: testAdminID, IsAdmin: true}
	owner := testAdminID
	sess, err := svc.Create(context.Background(), actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: profile.ID, Title: "t", Prompt: "hi",
		Origin: domain.OriginSchedule, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionWaiting)

	pending, err := repos.Approvals.PendingForSession(context.Background(), sess.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending approvals = %+v, err = %v", pending, err)
	}
	ap := pending[0]

	if err := svc.Decide(context.Background(), actor, ap.ID, true, nil, "human said yes"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	time.Sleep(50 * time.Millisecond) // outlive profile.ApprovalTimeout
	svc.RunMaintenance(context.Background())

	if len(h.Procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(h.Procs))
	}
	if n := len(h.Procs[0].Decisions); n != 1 {
		t.Fatalf("process saw %d decisions after expiry ran, want exactly 1 (no second decision): %+v", n, h.Procs[0].Decisions)
	}

	got, err := repos.Approvals.Get(context.Background(), ap.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if got.State != domain.ApprovalAllowed {
		t.Fatalf("approval state = %s, want allowed (the expiry sweep must not have touched it)", got.State)
	}
}
