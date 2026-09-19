package sessions

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

// Finding 2 regression test: when Approvals.Create fails, handlePermission
// must not leave the child hanging on an unanswered control_request. It
// must deny the request (so the child unblocks), log the failure, and fail
// the session (there is no pending approval left for a human to decide).
//
// Approvals.Create fails here on a real, primary-key UNIQUE constraint
// violation: a row is pre-inserted with a known id, and the service's
// (unexported, test-only-overridable) newApprovalID hook is pointed at
// that same id for the duration of the test. This is the least invasive
// way to force the failure deterministically — it does not require
// breaking the database connection (which would also break the
// assertions below, since they read the session and approvals back
// through the same repos) or adding any new production API.
func TestHandlePermission_ApprovalCreateFails_DeniesFailsSessionAndLogs(t *testing.T) {
	step := fake.Step{
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{RequestID: "req-conflict", ToolName: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)}},
		},
		WaitForDecision: true,
	}
	svc, repos, h := newService(t, step)
	logs := &recordingHandler{}
	svc.logger = slog.New(logs)

	owner := testAdminID
	ctx := context.Background()

	// A placeholder session to satisfy the approvals table's foreign key,
	// so the pre-inserted conflicting row is valid on its own terms.
	placeholder := domain.Session{
		ID: "placeholder-session", OwnerID: &owner, Title: "placeholder", WorkspaceID: testWorkspaceID,
		ProfileID: "interactive", Harness: "claude", State: domain.SessionOpen, Origin: domain.OriginUI,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := repos.Sessions.Create(ctx, placeholder); err != nil {
		t.Fatalf("create placeholder session: %v", err)
	}
	conflicting := domain.Approval{
		ID: "fixed-approval-id", SessionID: placeholder.ID, RequestID: "req-other", Tool: "Bash",
		Input: json.RawMessage(`{}`), Risk: domain.RiskRead, State: domain.ApprovalPending, CreatedAt: time.Now(),
	}
	if err := repos.Approvals.Create(ctx, conflicting); err != nil {
		t.Fatalf("create conflicting approval: %v", err)
	}

	svc.newApprovalID = func() string { return "fixed-approval-id" }

	sess, err := svc.Create(ctx, Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	waitForState(t, repos, sess.ID, domain.SessionFailed)

	if len(h.Procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(h.Procs))
	}
	decisions := h.Procs[0].Decisions
	if len(decisions) != 1 {
		t.Fatalf("expected exactly 1 decision sent to the child, got %+v", decisions)
	}
	if decisions[0].Allow {
		t.Errorf("decision = allow, want deny")
	}
	if decisions[0].RequestID != "req-conflict" {
		t.Errorf("decision request id = %q, want %q", decisions[0].RequestID, "req-conflict")
	}
	if decisions[0].Message != "styr could not record the approval" {
		t.Errorf("decision message = %q, want %q", decisions[0].Message, "styr could not record the approval")
	}

	if logs.count() == 0 {
		t.Error("expected the approval-create failure to be logged")
	}

	pending, err := repos.Approvals.PendingForSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("list pending for session: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no approval recorded for the failed session, got %+v", pending)
	}
}
