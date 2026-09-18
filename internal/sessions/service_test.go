package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

const (
	testAdminID     = "u-admin"
	testMemberID    = "u-member"
	testWorkspaceID = "ws-test"
	testSecret      = "0123456789abcdef0123456789abcdef" // 32+ bytes, for crypto.NewBox
)

// newService seeds a temp DB with an admin user holding a sealed Claude
// token, a member with no token, and a workspace pointing at a temp
// directory, then returns a Service wired to a fake harness that replays
// steps for every session it starts.
func newService(t *testing.T, steps ...fake.Step) (*Service, Repos, *fake.Harness) {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	repos := Repos{
		Sessions:   db.NewSessions(database),
		Events:     db.NewEvents(database),
		Approvals:  db.NewApprovals(database),
		Workspaces: db.NewWorkspaces(database),
		Profiles:   db.NewProfiles(database),
		Tokens:     db.NewTokens(database),
		Audit:      db.NewAudit(database),
	}
	users := db.NewUsers(database)

	box, err := crypto.NewBox(testSecret)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	admin := domain.User{ID: testAdminID, Issuer: "test", Subject: testAdminID, Email: "admin@example.com", DisplayName: "Admin", Role: domain.RoleAdmin, CreatedAt: now, LastLoginAt: now}
	member := domain.User{ID: testMemberID, Issuer: "test", Subject: testMemberID, Email: "member@example.com", DisplayName: "Member", Role: domain.RoleMember, CreatedAt: now, LastLoginAt: now}
	if err := users.Create(ctx, admin); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if err := users.Create(ctx, member); err != nil {
		t.Fatalf("create member user: %v", err)
	}

	ciphertext, nonce, err := box.Seal([]byte("sk-ant-test"))
	if err != nil {
		t.Fatalf("seal admin token: %v", err)
	}
	if err := repos.Tokens.Set(ctx, testAdminID, ciphertext, nonce, "test"); err != nil {
		t.Fatalf("set admin token: %v", err)
	}

	ws := domain.Workspace{ID: testWorkspaceID, Name: "test-ws", Path: t.TempDir(), DefaultProfileID: "interactive", CreatedAt: now}
	if err := repos.Workspaces.Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	h := fake.New(steps...)
	svc := New(repos, h, events.New(), box, Options{
		MaxOpen:     4,
		IdleTimeout: time.Hour,
		UsersDir:    t.TempDir(),
		ServiceHome: t.TempDir(),
	})
	return svc, repos, h
}

// waitForState polls repos for sess to reach want, failing the test if it
// does not within 5s.
func waitForState(t *testing.T, repos Repos, id string, want domain.SessionState) domain.Session {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sess, err := repos.Sessions.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get session %s: %v", id, err)
		}
		if sess.State == want {
			return *sess
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s: want state %s, still %s after 5s", id, want, sess.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func createResult(turns int) fake.Step {
	return fake.Step{Events: []harness.Event{{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: turns}}}}
}

// Rule 1: Create refuses when the owner has no Claude token.
func TestCreate_OwnerWithoutTokenRefused(t *testing.T) {
	svc, _, _ := newService(t)
	owner := testMemberID

	_, err := svc.Create(context.Background(), Actor{UserID: testMemberID}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), "add a Claude token in your profile") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// Rule 1 (unattended half) and rule 2: an owner-less session uses the
// service token and ServiceHome, and the token is passed only through the
// child process's env (domain.Session carries no token field at all).
func TestCreate_UnattendedUsesServiceToken(t *testing.T) {
	svc, repos, h := newService(t, createResult(1))

	box, err := crypto.NewBox(testSecret)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	ciphertext, nonce, err := box.Seal([]byte("sk-ant-service"))
	if err != nil {
		t.Fatalf("seal service token: %v", err)
	}
	if err := repos.Tokens.SetService(context.Background(), ciphertext, nonce, "svc"); err != nil {
		t.Fatalf("set service token: %v", err)
	}

	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "investigate", Title: "unattended", Prompt: "hi",
		Origin: domain.OriginSchedule, Owner: nil,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.OwnerID != nil {
		t.Errorf("owner = %v, want nil", sess.OwnerID)
	}

	if len(h.Procs) != 1 {
		t.Fatalf("expected 1 started process, got %d", len(h.Procs))
	}
	spec := h.Procs[0].Spec
	if spec.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "sk-ant-service" {
		t.Errorf("env token = %q, want the service token", spec.Env["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if info, err := os.Stat(spec.Home); err != nil || !info.IsDir() {
		t.Errorf("service home %q not created: %v", spec.Home, err)
	}
}

// Rule 3: every harness event is persisted except EventPartial, which is
// published on the bus (Seq 0) but never written to the events table.
func TestPump_PersistsAllEventsExceptPartial(t *testing.T) {
	step := fake.Step{Events: []harness.Event{
		{Type: harness.EventPartial, Text: "chun"},
		{Type: harness.EventText, Text: "chunk done"},
	}}
	svc, repos, _ := newService(t, step)
	ch, unsub := svc.bus.Subscribe(context.Background(), 32)
	defer unsub()

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var sawPartial, sawText bool
	deadline := time.After(5 * time.Second)
	for !sawPartial || !sawText {
		select {
		case msg := <-ch:
			if msg.SessionID != sess.ID || msg.Kind != "session.event" {
				continue
			}
			var ev harness.Event
			if err := json.Unmarshal(msg.Payload, &ev); err != nil {
				t.Fatalf("unmarshal event payload: %v", err)
			}
			switch ev.Type {
			case harness.EventPartial:
				sawPartial = true
				if msg.Seq != 0 {
					t.Errorf("partial seq = %d, want 0", msg.Seq)
				}
			case harness.EventText:
				sawText = true
				if msg.Seq == 0 {
					t.Errorf("text seq = 0, want nonzero")
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for bus events: sawPartial=%v sawText=%v", sawPartial, sawText)
		}
	}

	evs, err := repos.Events.ListAfter(context.Background(), sess.ID, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var sawTextRow bool
	for _, e := range evs {
		if e.Type == string(harness.EventPartial) {
			t.Errorf("partial event was persisted: %+v", e)
		}
		if e.Type == string(harness.EventText) {
			sawTextRow = true
		}
	}
	if !sawTextRow {
		t.Error("text event was not persisted")
	}
}

// Rule 4: a permission request creates a pending approval (classified by
// risk) and moves the session to waiting; Decide forwards the decision to
// the process, records it, moves the session back to running and writes an
// audit entry.
func TestApproval_CreateAndDecide(t *testing.T) {
	step := fake.Step{
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{RequestID: "req-1", ToolName: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)}},
			{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
		},
		WaitForDecision: true,
	}
	svc, repos, h := newService(t, step)

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
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
	if ap.Risk != domain.RiskRead {
		t.Errorf("risk = %s, want read", ap.Risk)
	}

	if err := svc.Decide(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, ap.ID, true, nil, ""); err != nil {
		t.Fatalf("decide: %v", err)
	}

	waitForState(t, repos, sess.ID, domain.SessionOpen)

	if len(h.Procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(h.Procs))
	}
	if len(h.Procs[0].Decisions) != 1 || !h.Procs[0].Decisions[0].Allow {
		t.Errorf("decision not forwarded to process: %+v", h.Procs[0].Decisions)
	}

	updated, err := repos.Approvals.Get(context.Background(), ap.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if updated.State != domain.ApprovalAllowed {
		t.Errorf("approval state = %s, want allowed", updated.State)
	}
	if updated.DecidedBy == nil || *updated.DecidedBy != testAdminID {
		t.Errorf("decided_by = %v, want %s", updated.DecidedBy, testAdminID)
	}

	audit, err := repos.Audit.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var found bool
	for _, a := range audit {
		if a.Action == "approval.allow" && a.Actor == testAdminID && a.Target == ap.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected an approval.allow audit entry")
	}
}

// Rule 5: a tool use records a "now" summary and moves the session to
// running; a result adds to the cumulative stats, clears the now line and
// moves the session to open.
func TestToolUseAndResult_UpdateStateAndStats(t *testing.T) {
	step := fake.Step{Events: []harness.Event{
		{Type: harness.EventToolUse, ToolUse: &harness.ToolUse{ID: "tu1", Name: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)}},
		{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 2, CostUSD: 0.05, InputTokens: 100, OutputTokens: 50}},
	}}
	svc, repos, _ := newService(t, step)
	ch, unsub := svc.bus.Subscribe(context.Background(), 32)
	defer unsub()

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var sawRunning, sawStats bool
	deadline := time.After(5 * time.Second)
	for !sawRunning || !sawStats {
		select {
		case msg := <-ch:
			if msg.SessionID != sess.ID {
				continue
			}
			switch msg.Kind {
			case "session.state":
				var body struct {
					State string `json:"state"`
				}
				_ = json.Unmarshal(msg.Payload, &body)
				if body.State == "running" {
					sawRunning = true
				}
			case "session.stats":
				sawStats = true
			}
		case <-deadline:
			t.Fatalf("timed out: sawRunning=%v sawStats=%v", sawRunning, sawStats)
		}
	}

	final := waitForState(t, repos, sess.ID, domain.SessionOpen)
	if final.NumTurns != 2 {
		t.Errorf("num_turns = %d, want 2", final.NumTurns)
	}
	if final.CostUSD != 0.05 {
		t.Errorf("cost_usd = %v, want 0.05", final.CostUSD)
	}
	if final.TokensIn != 100 || final.TokensOut != 50 {
		t.Errorf("tokens = (%d, %d), want (100, 50)", final.TokensIn, final.TokensOut)
	}
	if final.NowLine != "" {
		t.Errorf("now_line = %q, want cleared", final.NowLine)
	}
}

// Rule 5 (exit half): an exit with a non-zero code while the session is not
// being closed marks it failed.
func TestExit_NonZeroWhileNotClosing_MarksFailed(t *testing.T) {
	step := fake.Step{Events: []harness.Event{{Type: harness.EventExit, ExitCode: 1}}}
	svc, repos, _ := newService(t, step)

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	waitForState(t, repos, sess.ID, domain.SessionFailed)
}

// Rule 6: sending to a closed session starts a new process with Resume:
// true and the same session id.
func TestSend_OnClosedSession_ResumesProcess(t *testing.T) {
	svc, repos, h := newService(t, createResult(1))
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

	if err := svc.Close(context.Background(), actor, sess.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionClosed)

	if err := svc.Send(context.Background(), actor, sess.ID, "again"); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	if len(h.Procs) != 2 {
		t.Fatalf("expected 2 started processes, got %d", len(h.Procs))
	}
	if !h.Procs[1].Spec.Resume {
		t.Error("resumed process did not set Resume: true")
	}
	if h.Procs[1].Spec.SessionID != sess.ID {
		t.Errorf("resumed process session id = %q, want %q", h.Procs[1].Spec.SessionID, sess.ID)
	}
}

// Finding 3 regression test: two concurrent Sends to a closed session must
// not race to start two separate processes for it. Without the starting
// map guarding the procs[id]-absent-so-start-one TOCTOU, both goroutines
// could see no tracked process and both call startProcess, leaking a
// second harness process and sending Resume twice. Here exactly one
// process is started; the other Send waits for it and sends through the
// same process once it exists.
func TestSend_ConcurrentOnClosedSession_StartsExactlyOneProcess(t *testing.T) {
	svc, repos, h := newService(t, createResult(1))
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

	if err := svc.Close(context.Background(), actor, sess.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionClosed)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = svc.Send(context.Background(), actor, sess.ID, "again")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Send[%d] = %v, want nil", i, err)
		}
	}

	if len(h.Procs) != 2 {
		t.Fatalf("expected 2 total processes started (1 from Create, 1 from the winning resume), got %d", len(h.Procs))
	}

	resumed := h.Procs[1]
	if !resumed.Spec.Resume {
		t.Error("resumed process did not set Resume: true")
	}
	if resumed.Spec.SessionID != sess.ID {
		t.Errorf("resumed process session id = %q, want %q", resumed.Spec.SessionID, sess.ID)
	}
	if len(resumed.Sent) != 2 {
		t.Fatalf("expected both concurrent Sends to reach the single resumed process, got %d messages: %+v", len(resumed.Sent), resumed.Sent)
	}

	waitForState(t, repos, sess.ID, domain.SessionOpen)
}

// Rule 6 (conflict half): sending to a session waiting on an approval is
// refused.
func TestSend_WhileWaiting_ReturnsConflict(t *testing.T) {
	step := fake.Step{
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{RequestID: "req-1", ToolName: "Bash", Input: json.RawMessage(`{"command":"ls"}`)}},
			{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
		},
		WaitForDecision: true,
	}
	svc, repos, _ := newService(t, step)
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

	err = svc.Send(context.Background(), actor, sess.ID, "hurry up")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "answer the pending approval first") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// Rule 10: a session actor cannot see returns domain.ErrNotFound, an owned
// or owner-less session is visible, and admins see everything.
func TestVisibility(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	owner := testAdminID

	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi",
		Origin: domain.OriginUI, Owner: &owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.Get(context.Background(), Actor{UserID: testMemberID}, sess.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("member get owned session: want ErrNotFound, got %v", err)
	}
	if _, err := svc.Get(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, sess.ID); err != nil {
		t.Fatalf("admin get: %v", err)
	}
	if _, err := svc.Get(context.Background(), Actor{UserID: testAdminID}, sess.ID); err != nil {
		t.Fatalf("owner get: %v", err)
	}

	unowned := domain.Session{
		ID: uuid.NewString(), OwnerID: nil, Title: "unowned", WorkspaceID: testWorkspaceID,
		ProfileID: "interactive", Harness: "fake", State: domain.SessionOpen, Origin: domain.OriginSchedule,
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
	}
	if err := repos.Sessions.Create(context.Background(), unowned); err != nil {
		t.Fatalf("create raw session: %v", err)
	}
	if _, err := svc.Get(context.Background(), Actor{UserID: testMemberID}, unowned.ID); err != nil {
		t.Fatalf("member get owner-less session: %v", err)
	}
}

func TestSend_RecordsUserTurnAsEvent(t *testing.T) {
	svc, repos, _ := newService(t, fake.Step{Events: []harness.Event{{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}}}})
	owner := testAdminID
	actor := Actor{UserID: owner, IsAdmin: true}
	sess, err := svc.Create(context.Background(), actor, CreateInput{WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "first prompt", Origin: domain.OriginUI, Owner: &owner})
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)
	if err := svc.Send(context.Background(), actor, sess.ID, "second prompt"); err != nil {
		t.Fatal(err)
	}
	evs, err := repos.Events.ListAfter(context.Background(), sess.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, e := range evs {
		if e.Type == string(harness.EventUser) {
			var ev harness.Event
			_ = json.Unmarshal(e.Payload, &ev)
			texts = append(texts, ev.Text)
		}
	}
	if len(texts) != 2 || texts[0] != "first prompt" || texts[1] != "second prompt" {
		t.Fatalf("user turns = %v, want first and second prompt in order", texts)
	}
}
