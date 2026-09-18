package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

// slots.acquire itself: when every slot is busy and there is nothing to
// evict, it waits up to its configured timeout then returns ErrConflict.
// wait is shrunk here so the test does not take 30s.
func TestSlots_AcquireTimesOutWhenBusy(t *testing.T) {
	s := newSlots(1, nil)
	s.wait = 50 * time.Millisecond

	if err := s.acquire(context.Background(), false); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	err := s.acquire(context.Background(), false)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "all session slots busy") {
		t.Fatalf("unexpected message: %v", err)
	}

	s.release()
	if err := s.acquire(context.Background(), false); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

// Rule 7: with MaxOpen slots exhausted by an idle unattended session, an
// interactive Create evicts the oldest idle unattended process to free a
// slot instead of waiting out the full timeout.
func TestScheduler_InteractiveEvictsOldestIdleUnattended(t *testing.T) {
	svc, repos, h := newService(t, createResult(1))
	svc.slots = newSlots(1, svc.evictOldestIdleUnattended)

	actor := Actor{UserID: testAdminID, IsAdmin: true}
	owner := testAdminID

	sessA, err := svc.Create(context.Background(), actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "investigate", Title: "unattended", Prompt: "hi",
		Origin: domain.OriginSchedule, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	waitForState(t, repos, sessA.ID, domain.SessionOpen)

	sessB, err := svc.Create(context.Background(), actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "interactive", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create B (expected the scheduler to evict A and free a slot): %v", err)
	}

	waitForState(t, repos, sessA.ID, domain.SessionClosed)
	waitForState(t, repos, sessB.ID, domain.SessionOpen)

	if len(h.Procs) != 2 {
		t.Fatalf("expected 2 processes started, got %d", len(h.Procs))
	}
}

// Rule 8: RunMaintenance closes sessions that have been idle (state open)
// longer than IdleTimeout.
func TestRunMaintenance_ReapsIdleSessions(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	svc.opt.IdleTimeout = 20 * time.Millisecond

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	time.Sleep(50 * time.Millisecond)
	svc.RunMaintenance(context.Background())

	waitForState(t, repos, sess.ID, domain.SessionClosed)
}

// Rule 9: RunMaintenance denies pending approvals on unattended profiles
// once they are older than that profile's ApprovalTimeout, expiring the
// approval and reopening the session, with an audit entry attributed to
// "system".
func TestRunMaintenance_ExpiresStaleApprovals(t *testing.T) {
	step := fake.Step{
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{RequestID: "req-x", ToolName: "Bash", Input: json.RawMessage(`{"command":"rm -rf /tmp/x"}`)}},
			{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
		},
		WaitForDecision: true,
	}
	svc, repos, h := newService(t, step)

	profile := domain.Profile{ID: "test-unattended", Name: "test-unattended", Mode: "auto", Unattended: true, ApprovalTimeout: 20 * time.Millisecond}
	if err := repos.Profiles.Create(context.Background(), profile); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
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

	time.Sleep(50 * time.Millisecond)
	svc.RunMaintenance(context.Background())

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := repos.Approvals.Get(context.Background(), ap.ID)
		if err != nil {
			t.Fatalf("get approval: %v", err)
		}
		if got.State == domain.ApprovalExpired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("approval %s not expired, state = %s", ap.ID, got.State)
		}
		time.Sleep(10 * time.Millisecond)
	}

	waitForState(t, repos, sess.ID, domain.SessionOpen)

	if len(h.Procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(h.Procs))
	}
	if len(h.Procs[0].Decisions) != 1 || h.Procs[0].Decisions[0].Allow {
		t.Errorf("expected a deny decision forwarded to the process: %+v", h.Procs[0].Decisions)
	}
	if h.Procs[0].Decisions[0].Message != "timed out waiting for a human" {
		t.Errorf("decision message = %q", h.Procs[0].Decisions[0].Message)
	}

	audit, err := repos.Audit.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var found bool
	for _, a := range audit {
		if a.Actor == "system" && a.Target == ap.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected a system audit entry for the expired approval")
	}
}

// Finding 1 + 4 regression test: expireApprovals decides the database
// first (the guarded UPDATE is the sole arbiter that prevents the child
// from ever seeing two control_responses for the same request — finding
// 4), so a failure to notify the child afterward cannot undo that
// decision. expireApprovals must not discard that notify error: it must
// log it (via the service's *slog.Logger) and, since the process is
// unreachable and there is no pending approval left for anyone to retry,
// fail the session directly rather than leave it stuck in "waiting"
// forever. The approval itself ends up expired and audited regardless of
// whether the notify succeeded.
func TestExpireApprovals_NotifyProcessFails_ApprovalExpiresSessionFailsAndIsLogged(t *testing.T) {
	svc, repos, _ := newService(t)
	logs := &recordingHandler{}
	svc.logger = slog.New(logs)

	profile := domain.Profile{ID: "test-unattended-notify-fail", Name: "test-unattended-notify-fail", Mode: "auto", Unattended: true, ApprovalTimeout: time.Millisecond}
	if err := repos.Profiles.Create(context.Background(), profile); err != nil {
		t.Fatalf("create profile: %v", err)
	}

	owner := testAdminID
	sess := domain.Session{
		ID: uuid.NewString(), OwnerID: &owner, Title: "t", WorkspaceID: testWorkspaceID,
		ProfileID: profile.ID, Harness: "fake", State: domain.SessionWaiting, Origin: domain.OriginSchedule,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := repos.Sessions.Create(context.Background(), sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	ap := domain.Approval{
		ID: uuid.NewString(), SessionID: sess.ID, RequestID: "req-notify-fail", Tool: "Bash",
		Input: json.RawMessage(`{"command":"rm -rf /tmp/x"}`), Risk: domain.RiskDestructive,
		State: domain.ApprovalPending, CreatedAt: time.Now().Add(-time.Hour),
	}
	if err := repos.Approvals.Create(context.Background(), ap); err != nil {
		t.Fatalf("create approval: %v", err)
	}

	proc := newControlledProcess()
	proc.setDecideErr(errors.New("write to child: broken pipe"))
	if err := svc.registerProcess(context.Background(), sess.ID, &owner, proc); err != nil {
		t.Fatalf("register process: %v", err)
	}

	svc.expireApprovals(context.Background())

	got, err := repos.Approvals.Get(context.Background(), ap.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if got.State != domain.ApprovalExpired {
		t.Fatalf("approval state = %s, want expired (the database write is the source of truth, independent of the notify outcome)", got.State)
	}

	if n := len(proc.Decisions()); n != 0 {
		t.Fatalf("expected no successfully recorded decision on the process (Decide errored), got %d", n)
	}

	if logs.count() == 0 {
		t.Fatal("expected the notify failure to be logged, got no log records")
	}

	audit, err := repos.Audit.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var found bool
	for _, a := range audit {
		if a.Actor == "system" && a.Action == "approval.expire" && a.Target == ap.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a system approval.expire audit entry despite the failed notify")
	}

	final, err := repos.Sessions.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if final.State != domain.SessionFailed {
		t.Fatalf("session state = %s, want failed (the process is unreachable, so there is nothing left to retry)", final.State)
	}
}
