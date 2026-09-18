package triggers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
	"github.com/jonasthim/styr/internal/runs"
	"github.com/jonasthim/styr/internal/sessions"
)

const integrationSchema = `{"type":"object","required":["diagnosis"],"properties":{"diagnosis":{"type":"string"}}}`

// stack is the whole inbound path wired together: a real trigger router
// over a real run engine over a real sessions service over the fake
// harness, so one webhook exercises everything a delivered Grafana alert
// touches.
type stack struct {
	svc      *Service
	engine   *runs.Engine
	harness  *fake.Harness
	runs     *db.Runs
	sessions *db.Sessions
	trigger  domain.Trigger
	secret   string
}

func newStack(t *testing.T, steps ...fake.Step) *stack {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	box, err := crypto.NewBox("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new box: %v", err)
	}

	ctx := context.Background()
	sessionsRepo := db.NewSessions(database)
	eventsRepo := db.NewEvents(database)
	tokensRepo := db.NewTokens(database)
	templatesRepo := db.NewTemplates(database)
	triggersRepo := db.NewTriggers(database)
	deliveriesRepo := db.NewDeliveries(database)
	runsRepo := db.NewRuns(database)

	ciphertext, nonce, err := box.Seal([]byte("sk-ant-service"))
	if err != nil {
		t.Fatalf("seal token: %v", err)
	}
	if err := tokensRepo.SetService(ctx, ciphertext, nonce, "test"); err != nil {
		t.Fatalf("set service token: %v", err)
	}
	if err := db.NewUsers(database).Create(ctx, domain.User{
		ID: admin.UserID, Issuer: "test", Subject: admin.UserID, Email: "a@example.com",
		DisplayName: "Admin", Role: domain.RoleAdmin, CreatedAt: base, LastLoginAt: base,
	}); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := db.NewWorkspaces(database).Create(ctx, domain.Workspace{
		ID: "ws-1", Name: "ws", Path: t.TempDir(), DefaultProfileID: "investigate",
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	bus := events.New()
	h := fake.New(steps...)
	sessionsSvc := sessions.New(sessions.Repos{
		Sessions:   sessionsRepo,
		Events:     eventsRepo,
		Approvals:  db.NewApprovals(database),
		Workspaces: db.NewWorkspaces(database),
		Profiles:   db.NewProfiles(database),
		Tokens:     tokensRepo,
		Audit:      db.NewAudit(database),
	}, h, bus, box, sessions.Options{
		MaxOpen: 4, IdleTimeout: time.Hour, UsersDir: t.TempDir(), ServiceHome: t.TempDir(),
		Logger: slog.New(slog.DiscardHandler),
	})
	t.Cleanup(func() { _ = sessionsSvc.Shutdown(context.Background()) })

	engine := runs.New(runs.Repos{
		Runs:       runsRepo,
		Templates:  templatesRepo,
		Sessions:   sessionsRepo,
		Channels:   db.NewNotificationChannels(database),
		Deliveries: deliveriesRepo,
		Events:     eventsRepo,
	}, sessionsSvc, bus, nil, box, "https://styr.test", time.Hour, slog.New(slog.DiscardHandler))

	svc := New(Repos{Templates: templatesRepo, Triggers: triggersRepo, Deliveries: deliveriesRepo},
		engine, box, slog.New(slog.DiscardHandler), func() time.Time { return base })

	tpl := domain.Template{
		ID: uuid.NewString(), Name: "Grafana alert investigation", WorkspaceID: "ws-1", ProfileID: "investigate",
		TitleTemplate:  `{{ .status }}: {{ join ", " (alertnames .alerts) }}`,
		PromptTemplate: `A Grafana alert is {{ .status }}.{{ range .alerts }} {{ .labels.alertname }} on {{ .labels.instance }}.{{ end }}`,
		SystemPrompt:   "Investigate read-only.",
		ReportSchema:   integrationSchema,
		CreatedAt:      base, UpdatedAt: base,
	}
	if err := templatesRepo.Create(ctx, tpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	tr, secret, err := svc.CreateTrigger(ctx, admin, domain.TriggerInput{
		Name: "Grafana", Kind: "grafana", TemplateID: tpl.ID, Shared: true,
	})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	// The engine's bus loop is what follows a session to its outcome; serve
	// runs it in a goroutine and so does this test stack.
	loopCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.Run(loopCtx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return &stack{svc: svc, engine: engine, harness: h, runs: runsRepo, sessions: sessionsRepo, trigger: tr, secret: secret}
}

// TestDeliverEndToEndStartsAnUnattendedSession is the card's happy path: a
// Grafana webhook arrives, is accepted, and a real unattended session is
// running under the template's schema, pointing back at its run.
func TestDeliverEndToEndStartsAnUnattendedSession(t *testing.T) {
	s := newStack(t, fake.Step{})
	ctx := context.Background()

	dl, err := s.svc.Deliver(ctx, domain.Inbound{
		Slug:    s.trigger.Slug,
		Body:    grafanaPayload("firing", "abc123"),
		Headers: http.Header{"X-Styr-Secret": []string{s.secret}},
		Now:     base,
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if dl.Status != domain.DeliveryAccepted {
		t.Fatalf("status = %s (%s)", dl.Status, dl.Reason)
	}
	if dl.RunID == nil {
		t.Fatal("accepted delivery has no run id")
	}

	run, err := s.runs.Get(ctx, *dl.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Outcome != domain.RunRunning {
		t.Fatalf("outcome = %s, want running", run.Outcome)
	}
	if run.DeliveryID == nil || *run.DeliveryID != dl.ID {
		t.Fatalf("run delivery id = %v, want %s", run.DeliveryID, dl.ID)
	}
	if run.TriggerID == nil || *run.TriggerID != s.trigger.ID {
		t.Fatalf("run trigger id = %v", run.TriggerID)
	}

	sess, err := s.sessions.Get(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Origin != domain.OriginWebhook {
		t.Fatalf("origin = %s, want webhook", sess.Origin)
	}
	if sess.OriginRef != run.ID {
		t.Fatalf("origin_ref = %q, want the run id %q", sess.OriginRef, run.ID)
	}
	if sess.OwnerID != nil {
		t.Fatal("an unattended session must have no owner")
	}
	if sess.Title != "firing: SystemdUnitFailed" {
		t.Fatalf("title = %q", sess.Title)
	}

	if len(s.harness.Procs) != 1 {
		t.Fatalf("started %d processes", len(s.harness.Procs))
	}
	spec := s.harness.Procs[0].Spec
	if spec.JSONSchema != integrationSchema {
		t.Fatalf("StartSpec.JSONSchema = %q", spec.JSONSchema)
	}
	if spec.SystemPrompt != "Investigate read-only." {
		t.Fatalf("StartSpec.SystemPrompt = %q", spec.SystemPrompt)
	}
	sent := s.harness.Procs[0].Sent
	if len(sent) != 1 || sent[0].Text != "A Grafana alert is firing. SystemdUnitFailed on ct-142." {
		t.Fatalf("prompt sent = %+v", sent)
	}

	// The run view the API serves carries the delivery payload back.
	view, err := s.engine.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("engine.Get: %v", err)
	}
	if view.Delivery == nil || view.Delivery.ID != dl.ID {
		t.Fatalf("view delivery = %+v", view.Delivery)
	}
	if view.Session == nil || view.Template == nil {
		t.Fatalf("view = %+v", view)
	}
}

// TestDeliverEndToEndReachesSuccess drives the same path with a session
// that answers immediately, so the run closes out with the structured
// report the schema asked for.
func TestDeliverEndToEndReachesSuccess(t *testing.T) {
	report := `{"diagnosis":"the ssh unit failed to start"}`
	s := newStack(t, fake.Step{Events: []harness.Event{{
		Type: harness.EventResult,
		Result: &harness.Result{
			Subtype: "success", NumTurns: 1, CostUSD: 0.12, Text: "done",
			StructuredOutput: json.RawMessage(report),
		},
	}}})
	ctx := context.Background()

	dl, err := s.svc.Deliver(ctx, domain.Inbound{
		Slug:    s.trigger.Slug,
		Body:    grafanaPayload("firing", "abc123"),
		Headers: http.Header{"X-Styr-Secret": []string{s.secret}},
		Now:     base,
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if dl.RunID == nil {
		t.Fatal("no run id")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := s.runs.Get(ctx, *dl.RunID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Outcome == domain.RunSuccess {
			if string(run.Report) != report {
				t.Fatalf("report = %s", run.Report)
			}
			if run.Summary != "the ssh unit failed to start" {
				t.Fatalf("summary = %q", run.Summary)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run outcome = %s after 5s, want success", run.Outcome)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
