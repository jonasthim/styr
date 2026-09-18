package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

// Create passes the requested model and effort to the harness and stores them on the row.
func TestCreate_ModelAndEffortReachTheHarness(t *testing.T) {
	svc, repos, h := newService(t, createResult(1))
	owner := testAdminID

	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner, Model: "opus", Effort: "high",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(h.Procs) != 1 {
		t.Fatalf("expected 1 started process, got %d", len(h.Procs))
	}
	if got := h.Procs[0].Spec; got.Model != "opus" || got.Effort != "high" {
		t.Errorf("spec model/effort = %q/%q, want opus/high", got.Model, got.Effort)
	}
	stored, err := repos.Sessions.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Effort != "high" {
		t.Errorf("stored effort = %q, want high", stored.Effort)
	}
}

// With nothing requested, the profile's defaults apply (investigate is seeded sonnet/medium).
func TestCreate_FallsBackToProfileModelAndEffort(t *testing.T) {
	svc, _, h := newService(t, createResult(1))
	owner := testAdminID

	if _, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "investigate", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := h.Procs[0].Spec; got.Model != "sonnet" || got.Effort != "medium" {
		t.Errorf("spec model/effort = %q/%q, want the investigate profile's sonnet/medium", got.Model, got.Effort)
	}
}

// The interactive profile seeds neither, so the CLI's own defaults stand: no flags at all.
func TestCreate_InteractiveProfileLeavesCLIDefaults(t *testing.T) {
	svc, _, h := newService(t, createResult(1))
	owner := testAdminID

	if _, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := h.Procs[0].Spec; got.Model != "" || got.Effort != "" {
		t.Errorf("spec model/effort = %q/%q, want both empty", got.Model, got.Effort)
	}
}

func TestCreate_RejectsUnknownEffort(t *testing.T) {
	svc, _, h := newService(t, createResult(1))
	owner := testAdminID

	_, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner, Effort: "turbo",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create with an unknown effort: err = %v, want ErrInvalid", err)
	}
	if len(h.Procs) != 0 {
		t.Fatalf("expected no process started, got %d", len(h.Procs))
	}
}

// SwitchModel closes the live process and resumes the same session id under the new flags.
func TestSwitchModel_ResumesWithNewModelAndEffort(t *testing.T) {
	svc, repos, h := newService(t, createResult(1))
	actor := Actor{UserID: testAdminID, IsAdmin: true}
	owner := testAdminID

	sess, err := svc.Create(context.Background(), actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner, Model: "sonnet", Effort: "medium",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	if err := svc.SwitchModel(context.Background(), actor, sess.ID, "opus", "high"); err != nil {
		t.Fatalf("switch model: %v", err)
	}

	if len(h.Procs) != 2 {
		t.Fatalf("expected 2 started processes, got %d", len(h.Procs))
	}
	second := h.Procs[1].Spec
	if !second.Resume {
		t.Error("second process did not set Resume: true")
	}
	if second.SessionID != sess.ID {
		t.Errorf("second process session id = %q, want %q", second.SessionID, sess.ID)
	}
	if second.Model != "opus" || second.Effort != "high" {
		t.Errorf("second process model/effort = %q/%q, want opus/high", second.Model, second.Effort)
	}
	// No invented user turn: the resumed process waits for the operator's next message.
	if len(h.Procs[1].Sent) != 0 {
		t.Errorf("resumed process was sent %+v, want nothing", h.Procs[1].Sent)
	}

	stored, err := repos.Sessions.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if stored.Model != "opus" || stored.Effort != "high" {
		t.Errorf("stored model/effort = %q/%q, want opus/high", stored.Model, stored.Effort)
	}
	if stored.State != domain.SessionRunning {
		t.Errorf("state = %s, want running after the resume", stored.State)
	}
}

// A session waiting on an approval must not be restarted: the child is blocked on a
// control_request only the current process can answer.
func TestSwitchModel_WhileWaitingConflicts(t *testing.T) {
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

	err = svc.SwitchModel(context.Background(), actor, sess.ID, "opus", "high")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("SwitchModel while waiting: err = %v, want ErrConflict", err)
	}
	if len(h.Procs) != 1 {
		t.Fatalf("expected the original process only, got %d", len(h.Procs))
	}
}

func TestSwitchModel_RejectsUnknownEffort(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	actor := Actor{UserID: testAdminID, IsAdmin: true}
	owner := testAdminID

	sess, err := svc.Create(context.Background(), actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	if err := svc.SwitchModel(context.Background(), actor, sess.ID, "opus", "turbo"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("SwitchModel with an unknown effort: err = %v, want ErrInvalid", err)
	}
}

func TestSwitchModel_InvisibleSessionNotFound(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	owner := testAdminID

	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	err = svc.SwitchModel(context.Background(), Actor{UserID: testMemberID}, sess.ID, "opus", "")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SwitchModel on someone else's session: err = %v, want ErrNotFound", err)
	}
}

// The init message's slash_commands list is stored on the session row.
func TestPump_StoresSlashCommandsFromInit(t *testing.T) {
	step := fake.Step{Events: []harness.Event{
		{Type: harness.EventInit, Init: &harness.Init{SessionID: "s", Model: "claude-fable-5-1", SlashCommands: []string{"compact", "commit-commands:commit"}}},
		{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
	}}
	svc, repos, _ := newService(t, step)
	owner := testAdminID

	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	stored, err := repos.Sessions.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(stored.SlashCommands) != 2 || stored.SlashCommands[0] != "compact" {
		t.Errorf("slash commands = %v, want [compact commit-commands:commit]", stored.SlashCommands)
	}
	if stored.Model != "claude-fable-5-1" {
		t.Errorf("model = %q, want the model the CLI reported", stored.Model)
	}
}
