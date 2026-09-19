package sessions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

// withCodex registers a second fake, standing in for the Codex CLI, on a service built by
// newService, and seals a Codex key for the admin so codex sessions can start. It returns the
// codex fake so a test can inspect the spec its processes were started with.
func withCodex(t *testing.T, svc *Service, repos Repos, steps ...fake.Step) *fake.Harness {
	t.Helper()
	codexFake := fake.NewKind(harness.KindCodex, steps...)
	svc.reg.Register(codexFake)

	box, err := crypto.NewBox(testSecret)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	ciphertext, nonce, err := box.Seal([]byte("sk-test-codex-key"))
	if err != nil {
		t.Fatalf("seal codex key: %v", err)
	}
	if err := repos.CodexCredentials.Set(context.Background(), testAdminID, ciphertext, nonce, "…-key"); err != nil {
		t.Fatalf("set codex key: %v", err)
	}
	return codexFake
}

// setProfileHarness points a builtin profile at a harness kind, the way an admin would from
// the settings table.
func setProfileHarness(t *testing.T, repos Repos, profileID string, kind harness.Kind) {
	t.Helper()
	ctx := context.Background()
	p, err := repos.Profiles.Get(ctx, profileID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	p.Harness = string(kind)
	if err := repos.Profiles.Update(ctx, *p); err != nil {
		t.Fatalf("update profile: %v", err)
	}
}

// A session with no explicit harness runs on whatever its profile names.
func TestCreate_HarnessDefaultsToTheProfiles(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	codexFake := withCodex(t, svc, repos, createResult(1))
	setProfileHarness(t, repos, "interactive", harness.KindCodex)

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi", Owner: &owner,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.Harness != string(harness.KindCodex) {
		t.Fatalf("session harness = %q, want codex", sess.Harness)
	}
	if len(codexFake.Procs) != 1 {
		t.Fatalf("codex harness started %d processes, want 1", len(codexFake.Procs))
	}
}

// The request's own choice wins over the profile's default.
func TestCreate_HarnessFromRequestOverridesProfile(t *testing.T) {
	svc, repos, claudeFake := newService(t, createResult(1))
	codexFake := withCodex(t, svc, repos, createResult(1))

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.Harness != string(harness.KindCodex) {
		t.Fatalf("session harness = %q, want codex", sess.Harness)
	}
	if len(codexFake.Procs) != 1 || len(claudeFake.Procs) != 0 {
		t.Fatalf("started codex=%d claude=%d, want 1 and 0", len(codexFake.Procs), len(claudeFake.Procs))
	}
}

// With neither a request choice nor a profile default, a session is a Claude Code session.
func TestCreate_HarnessFallsBackToClaude(t *testing.T) {
	svc, _, claudeFake := newService(t, createResult(1))

	owner := testAdminID
	sess, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Title: "t", Prompt: "hi", Owner: &owner,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.Harness != string(harness.KindClaude) {
		t.Fatalf("session harness = %q, want claude", sess.Harness)
	}
	if len(claudeFake.Procs) != 1 {
		t.Fatalf("claude harness started %d processes, want 1", len(claudeFake.Procs))
	}
}

func TestCreate_UnknownHarnessRejected(t *testing.T) {
	svc, _, _ := newService(t)
	owner := testAdminID
	_, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: "gemini",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create with an unknown harness = %v, want ErrInvalid", err)
	}
}

// A harness Styr knows about but this build has no implementation for is refused before a
// session row is written, the same way a missing credential is.
func TestCreate_UnregisteredHarnessRejected(t *testing.T) {
	svc, repos, _ := newService(t)
	owner := testAdminID
	_, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create on a build without codex = %v, want ErrInvalid", err)
	}
	list, err := repos.Sessions.ListVisible(context.Background(), testAdminID, true)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("a session row was written for a harness that cannot run: %d rows", len(list))
	}
}

// A Codex session needs the owner's OpenAI API key, not their Claude token.
func TestCreate_CodexRequiresTheOwnersKey(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	codexFake := fake.NewKind(harness.KindCodex, createResult(1))
	svc.reg.Register(codexFake)

	owner := testAdminID // has a Claude token, no Codex key
	_, err := svc.Create(context.Background(), Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create without a codex key = %v, want ErrInvalid", err)
	}
	if list, _ := repos.Sessions.ListVisible(context.Background(), testAdminID, true); len(list) != 0 {
		t.Fatalf("a session row was written without a credential: %d rows", len(list))
	}
}

// An unattended Codex session (no owner) runs on the service-wide key.
func TestCreate_CodexUnattendedUsesTheServiceKey(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	codexFake := fake.NewKind(harness.KindCodex, createResult(1))
	svc.reg.Register(codexFake)

	ctx := context.Background()
	_, err := svc.Create(ctx, Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi",
		Harness: harness.KindCodex, Origin: domain.OriginSchedule,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unattended Create without a service codex key = %v, want ErrInvalid", err)
	}

	box, err := crypto.NewBox(testSecret)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	ciphertext, nonce, err := box.Seal([]byte("sk-service-codex-key"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := repos.CodexCredentials.SetService(ctx, ciphertext, nonce, "…-key"); err != nil {
		t.Fatalf("SetService: %v", err)
	}
	if _, err := svc.Create(ctx, Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi",
		Harness: harness.KindCodex, Origin: domain.OriginSchedule,
	}); err != nil {
		t.Fatalf("unattended Create with a service codex key: %v", err)
	}
}

// The credential goes into the child's environment under the name that harness expects, and
// nothing else is handed over: a Codex child never sees a Claude token, or the reverse.
func TestStartProcess_CredentialEnvironmentPerHarness(t *testing.T) {
	svc, repos, claudeFake := newService(t, createResult(1))
	codexFake := withCodex(t, svc, repos, createResult(1))

	ctx := context.Background()
	owner := testAdminID
	actor := Actor{UserID: testAdminID, IsAdmin: true}
	if _, err := svc.Create(ctx, actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
	}); err != nil {
		t.Fatalf("create claude session: %v", err)
	}
	if _, err := svc.Create(ctx, actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex,
	}); err != nil {
		t.Fatalf("create codex session: %v", err)
	}

	claudeEnv := claudeFake.Procs[0].Spec.Env
	if claudeEnv["CLAUDE_CODE_OAUTH_TOKEN"] == "" {
		t.Fatal("the claude child got no CLAUDE_CODE_OAUTH_TOKEN")
	}
	if _, ok := claudeEnv["OPENAI_API_KEY"]; ok {
		t.Fatal("the claude child was handed an OPENAI_API_KEY")
	}

	codexEnv := codexFake.Procs[0].Spec.Env
	if codexEnv["OPENAI_API_KEY"] != "sk-test-codex-key" {
		t.Fatalf("the codex child got OPENAI_API_KEY %q", codexEnv["OPENAI_API_KEY"])
	}
	if _, ok := codexEnv["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Fatal("the codex child was handed a Claude token")
	}
	if len(codexEnv) != 1 {
		t.Fatalf("the codex child's credential env = %v, want only OPENAI_API_KEY", keysOf(codexEnv))
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Codex decides tool use with its sandbox policy and never asks the host, so there is nothing
// to approve: Decide says so rather than reaching a process that would only return
// ErrUnsupported.
func TestDecide_CodexSessionHasNoApprovals(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	withCodex(t, svc, repos, createResult(1))

	ctx := context.Background()
	owner := testAdminID
	actor := Actor{UserID: testAdminID, IsAdmin: true}
	sess, err := svc.Create(ctx, actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ap := domain.Approval{
		ID: "ap-codex", SessionID: sess.ID, RequestID: "req-1", Tool: "Bash",
		Input: []byte(`{}`), Risk: domain.RiskExec, State: domain.ApprovalPending, CreatedAt: time.Now(),
	}
	if err := repos.Approvals.Create(ctx, ap); err != nil {
		t.Fatalf("create approval: %v", err)
	}

	err = svc.Decide(ctx, actor, ap.ID, true, nil, "")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Decide on a codex session = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "no approvals") {
		t.Fatalf("Decide error = %v, want it to say this harness has no approvals", err)
	}
	// The approval is untouched: nothing was decided.
	got, err := repos.Approvals.Get(ctx, ap.ID)
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if got.State != domain.ApprovalPending {
		t.Fatalf("approval state = %s, want it left pending", got.State)
	}
}

// Switching model on a Codex session restarts its process under the new flags, on the same
// harness, without inventing a user turn.
func TestSwitchModel_CodexSession(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	codexFake := withCodex(t, svc, repos, createResult(1), createResult(1))

	ctx := context.Background()
	owner := testAdminID
	actor := Actor{UserID: testAdminID, IsAdmin: true}
	sess, err := svc.Create(ctx, actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForState(t, repos, sess.ID, domain.SessionOpen)

	if err := svc.SwitchModel(ctx, actor, sess.ID, "gpt-5-codex", ""); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	if len(codexFake.Procs) != 2 {
		t.Fatalf("codex processes = %d, want a second one after the switch", len(codexFake.Procs))
	}
	second := codexFake.Procs[1].Spec
	if second.Model != "gpt-5-codex" {
		t.Fatalf("resumed spec model = %q, want gpt-5-codex", second.Model)
	}
	if !second.Resume {
		t.Fatal("the resumed process was not started with Resume")
	}
	after, err := repos.Sessions.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if after.Harness != string(harness.KindCodex) {
		t.Fatalf("harness after switch = %q, want codex", after.Harness)
	}
}

// Interrupt reaches the Codex process like any other.
func TestInterrupt_CodexSession(t *testing.T) {
	svc, repos, _ := newService(t, createResult(1))
	codexFake := withCodex(t, svc, repos, createResult(1))

	ctx := context.Background()
	owner := testAdminID
	actor := Actor{UserID: testAdminID, IsAdmin: true}
	sess, err := svc.Create(ctx, actor, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Interrupt(ctx, actor, sess.ID); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	// The fake emits a synthetic interrupted result, which the pump turns into an "open"
	// session; waiting for that is what makes reading Interrupts race-free.
	waitForState(t, repos, sess.ID, domain.SessionOpen)
	if got := codexFake.Procs[0].Interrupts; got != 1 {
		t.Fatalf("interrupts = %d, want 1", got)
	}
}

// The init message is the authority on which CLI answered: the pump stores what it reported.
func TestPump_StoresHarnessFromInit(t *testing.T) {
	svc, repos, _ := newService(t, fake.Step{Events: []harness.Event{
		{Type: harness.EventInit, Init: &harness.Init{Harness: harness.KindClaude, SessionID: "s", Model: "claude-fable-5-1"}},
		{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
	}})

	ctx := context.Background()
	owner := testAdminID
	sess, err := svc.Create(ctx, Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := waitForState(t, repos, sess.ID, domain.SessionOpen)
	if got.Harness != string(harness.KindClaude) {
		t.Fatalf("harness = %q, want claude", got.Harness)
	}
	if got.Model != "claude-fable-5-1" {
		t.Fatalf("model = %q, want the one init reported", got.Model)
	}
}

// A harness whose init message names no model (Codex only echoes back what Styr asked for)
// must not blank the session's model column.
func TestPump_EmptyInitModelKeepsTheSessionsModel(t *testing.T) {
	svc, repos, _ := newService(t)
	withCodex(t, svc, repos, fake.Step{Events: []harness.Event{
		{Type: harness.EventInit, Init: &harness.Init{Harness: harness.KindCodex, SessionID: "thread-1"}},
		{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
	}})

	ctx := context.Background()
	owner := testAdminID
	sess, err := svc.Create(ctx, Actor{UserID: testAdminID, IsAdmin: true}, CreateInput{
		WorkspaceID: testWorkspaceID, ProfileID: "interactive", Prompt: "hi", Owner: &owner,
		Harness: harness.KindCodex, Model: "gpt-5-codex",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := waitForState(t, repos, sess.ID, domain.SessionOpen)
	if got.Model != "gpt-5-codex" {
		t.Fatalf("model = %q, want the requested gpt-5-codex", got.Model)
	}
	if got.Harness != string(harness.KindCodex) {
		t.Fatalf("harness = %q, want codex", got.Harness)
	}
}
