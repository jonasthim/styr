package runs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	"github.com/jonasthim/styr/internal/notify"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/templates"
)

const (
	testSecret       = "0123456789abcdef0123456789abcdef" // 32+ bytes, for crypto.NewBox
	testWorkspace    = "ws-runs"
	testReportSchema = `{"type":"object","properties":{"diagnosis":{"type":"string"}}}`
)

// env is everything a run-engine test needs to poke at the world the
// engine runs in.
type env struct {
	engine     *Engine
	sessions   *sessions.Service
	repos      Repos
	harness    *fake.Harness
	bus        *events.Bus
	templateID string
	channels   *db.NotificationChannels
	box        *crypto.Box
}

// newEnv wires a real sessions service over the fake harness, a real run
// engine over a temp database, and a seeded template that renders a prompt
// from a payload.
func newEnv(t *testing.T, timeout time.Duration, notifier Notifier, steps ...fake.Step) *env {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	box, err := crypto.NewBox(testSecret)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}

	ctx := context.Background()
	sessionsRepo := db.NewSessions(database)
	eventsRepo := db.NewEvents(database)
	tokensRepo := db.NewTokens(database)
	workspacesRepo := db.NewWorkspaces(database)
	templatesRepo := db.NewTemplates(database)
	channelsRepo := db.NewNotificationChannels(database)

	ciphertext, nonce, err := box.Seal([]byte("sk-ant-service"))
	if err != nil {
		t.Fatalf("seal service token: %v", err)
	}
	if err := tokensRepo.SetService(ctx, ciphertext, nonce, "test"); err != nil {
		t.Fatalf("set service token: %v", err)
	}

	now := time.Now()
	if err := workspacesRepo.Create(ctx, domain.Workspace{
		ID: testWorkspace, Name: "runs-ws", Path: t.TempDir(), DefaultProfileID: "investigate",
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	tpl := domain.Template{
		ID: uuid.NewString(), Name: "Alert investigation", WorkspaceID: testWorkspace, ProfileID: "investigate",
		TitleTemplate:  `{{ .status }}: alert`,
		PromptTemplate: `Investigate {{ .status }}.`,
		SystemPrompt:   "Read only.",
		ReportSchema:   testReportSchema,
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := templatesRepo.Create(ctx, tpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	bus := events.New()
	h := fake.New(steps...)
	sessionsSvc := sessions.New(sessions.Repos{
		Sessions:   sessionsRepo,
		Events:     eventsRepo,
		Approvals:  db.NewApprovals(database),
		Workspaces: workspacesRepo,
		Profiles:   db.NewProfiles(database),
		Tokens:     tokensRepo,
		Audit:      db.NewAudit(database),
	}, h, bus, box, sessions.Options{
		MaxOpen: 4, IdleTimeout: time.Hour, UsersDir: t.TempDir(), ServiceHome: t.TempDir(),
		Logger: slog.New(slog.DiscardHandler),
	})
	t.Cleanup(func() { _ = sessionsSvc.Shutdown(context.Background()) })

	repos := Repos{
		Runs:       db.NewRuns(database),
		Templates:  templatesRepo,
		Sessions:   sessionsRepo,
		Channels:   channelsRepo,
		Deliveries: db.NewDeliveries(database),
		Events:     eventsRepo,
		Loops:      db.NewLoops(database),
	}
	engine := New(repos, sessionsSvc, bus, notifier, box, "https://styr.test/", timeout, slog.New(slog.DiscardHandler))

	return &env{
		engine: engine, sessions: sessionsSvc, repos: repos, harness: h, bus: bus,
		templateID: tpl.ID, channels: channelsRepo, box: box,
	}
}

// startLoop runs the engine's bus loop for the duration of the test.
func (e *env) startLoop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.engine.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// recorder is a Notifier that captures what would have been published.
type recorder struct {
	mu       sync.Mutex
	events   []notify.Event
	channels [][]notify.Channel
}

func (r *recorder) Publish(_ context.Context, channels []notify.Channel, ev notify.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	r.channels = append(r.channels, channels)
}

func (r *recorder) snapshot() []notify.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]notify.Event(nil), r.events...)
}

func resultStep(res *harness.Result) fake.Step {
	return fake.Step{Events: []harness.Event{{Type: harness.EventResult, Result: res}}}
}

// waitForRun polls until the run reaches want, or fails the test.
func waitForRun(t *testing.T, repos Repos, id string, want domain.RunOutcome) domain.Run {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		run, err := repos.Runs.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get run %s: %v", id, err)
		}
		if run.Outcome == want {
			return *run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s: want outcome %s, still %s after 5s", id, want, run.Outcome)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForEvents(t *testing.T, rec *recorder, n int) []notify.Event {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if evs := rec.snapshot(); len(evs) >= n {
			return evs
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d notifications, got %d after 5s", n, len(rec.snapshot()))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func vars(status string) templates.Vars { return templates.Vars{"status": status} }

func TestStartCreatesUnattendedSessionFromTemplate(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, fake.Step{})
	e.startLoop(t)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.Outcome != domain.RunRunning {
		t.Fatalf("outcome = %s, want running", run.Outcome)
	}

	sess, err := e.repos.Sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.OwnerID != nil {
		t.Fatalf("owner = %v, want nil (unattended)", *sess.OwnerID)
	}
	if sess.Origin != domain.OriginWebhook {
		t.Fatalf("origin = %s, want webhook", sess.Origin)
	}
	if sess.OriginRef != run.ID {
		t.Fatalf("origin_ref = %q, want the run id %q", sess.OriginRef, run.ID)
	}
	if sess.Title != "firing: alert" {
		t.Fatalf("title = %q", sess.Title)
	}

	if len(e.harness.Procs) != 1 {
		t.Fatalf("started %d processes, want 1", len(e.harness.Procs))
	}
	spec := e.harness.Procs[0].Spec
	if spec.JSONSchema != testReportSchema {
		t.Fatalf("StartSpec.JSONSchema = %q", spec.JSONSchema)
	}
	if spec.SystemPrompt != "Read only." {
		t.Fatalf("StartSpec.SystemPrompt = %q", spec.SystemPrompt)
	}
	if got := e.harness.Procs[0].Sent; len(got) != 1 || got[0].Text != "Investigate firing." {
		t.Fatalf("sent = %+v", got)
	}
}

func TestStartMissingTemplate(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{})
	if _, err := e.engine.Start(context.Background(), RunInput{TemplateID: "nope"}); err == nil {
		t.Fatal("want an error for an unknown template")
	}
}

func TestRunFinishesSuccessWithStructuredOutputAndNotifiesNtfy(t *testing.T) {
	type captured struct {
		title, priority, tags, body, click string
	}
	got := make(chan captured, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		got <- captured{
			title:    r.Header.Get("Title"),
			priority: r.Header.Get("Priority"),
			tags:     r.Header.Get("Tags"),
			click:    r.Header.Get("Click"),
			body:     string(buf[:n]),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	report := `{"severity":"warning","diagnosis":"the unit crashed on boot","confidence":0.8}`
	e := newEnv(t, time.Hour, nil, resultStep(&harness.Result{
		Subtype: "success", NumTurns: 2, CostUSD: 0.42, Text: "all done",
		StructuredOutput: json.RawMessage(report),
	}))
	e.engine.notifier = notify.New(srv.Client(), slog.New(slog.DiscardHandler))
	if err := e.channels.Create(context.Background(), domain.NotificationChannel{
		ID: uuid.NewString(), Kind: domain.ChannelNtfy, Name: "homelab", URL: srv.URL,
		Events: []string{EventFinished, EventFailed}, Enabled: true, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	e.startLoop(t)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	finished := waitForRun(t, e.repos, run.ID, domain.RunSuccess)

	if string(finished.Report) != report {
		t.Fatalf("report = %s", finished.Report)
	}
	if finished.Summary != "the unit crashed on boot" {
		t.Fatalf("summary = %q", finished.Summary)
	}
	if finished.CostUSD != 0.42 {
		t.Fatalf("cost = %v", finished.CostUSD)
	}
	if finished.FinishedAt == nil {
		t.Fatal("finished_at is nil")
	}

	select {
	case c := <-got:
		if c.title != "success: firing: alert" {
			t.Fatalf("Title header = %q", c.title)
		}
		if c.priority != "3" {
			t.Fatalf("Priority header = %q", c.priority)
		}
		if c.body != "the unit crashed on boot" {
			t.Fatalf("body = %q", c.body)
		}
		if c.click != "https://styr.test/runs/"+run.ID {
			t.Fatalf("Click header = %q", c.click)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no ntfy notification within 5s")
	}
}

func TestRunWithoutStructuredOutputWrapsResultText(t *testing.T) {
	rec := &recorder{}
	e := newEnv(t, time.Hour, rec, resultStep(&harness.Result{Subtype: "success", NumTurns: 1, Text: "nothing to report"}))
	e.startLoop(t)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	finished := waitForRun(t, e.repos, run.ID, domain.RunSuccess)

	var report map[string]string
	if err := json.Unmarshal(finished.Report, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report["result"] != "nothing to report" {
		t.Fatalf("report = %s", finished.Report)
	}
	if finished.Summary != "nothing to report" {
		t.Fatalf("summary = %q", finished.Summary)
	}
}

func TestRunFailsWhenSessionFails(t *testing.T) {
	rec := &recorder{}
	// An exit with a non-zero code that styr did not ask for fails the
	// session, which fails the run.
	e := newEnv(t, time.Hour, rec, fake.Step{Events: []harness.Event{{Type: harness.EventExit, ExitCode: 2}}})
	e.startLoop(t)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	finished := waitForRun(t, e.repos, run.ID, domain.RunFailed)
	if finished.Summary != "the session failed" {
		t.Fatalf("summary = %q", finished.Summary)
	}

	evs := waitForEvents(t, rec, 1)
	if evs[0].Kind != EventFailed {
		t.Fatalf("event kind = %q", evs[0].Kind)
	}
	if evs[0].Priority != 4 {
		t.Fatalf("priority = %d, want 4", evs[0].Priority)
	}
}

func TestTickTimesOutAnOldRun(t *testing.T) {
	rec := &recorder{}
	// No scripted step: the fake synthesizes a result for the first Send,
	// so the run would otherwise succeed — the loop is deliberately not
	// started, leaving the run in `running` for the sweep to find.
	e := newEnv(t, time.Millisecond, rec, fake.Step{})

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	e.engine.Tick(context.Background(), time.Now().Add(time.Minute))

	finished, err := e.repos.Runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if finished.Outcome != domain.RunTimeout {
		t.Fatalf("outcome = %s, want timeout", finished.Outcome)
	}
	if len(e.harness.Procs) != 1 || e.harness.Procs[0].Interrupts == 0 {
		t.Fatalf("want the session interrupted, procs = %+v", e.harness.Procs)
	}

	evs := waitForEvents(t, rec, 1)
	if evs[0].Kind != EventFailed {
		t.Fatalf("event kind = %q", evs[0].Kind)
	}
	if evs[0].Title != "timeout: firing: alert" {
		t.Fatalf("title = %q", evs[0].Title)
	}

	// A second sweep must not re-close (or re-notify) the same run.
	e.engine.Tick(context.Background(), time.Now().Add(time.Hour))
	if got := len(rec.snapshot()); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
}

func TestTickIgnoresYoungRuns(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, fake.Step{})
	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.engine.Tick(context.Background(), time.Now())
	got, err := e.repos.Runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Outcome != domain.RunRunning {
		t.Fatalf("outcome = %s, want running", got.Outcome)
	}
}

func TestNeedsHumanNotifiedOncePerRun(t *testing.T) {
	rec := &recorder{}
	permission := harness.Event{
		Type:       harness.EventPermission,
		Permission: &harness.PermissionRequest{RequestID: "req-1", ToolName: "Bash", Input: json.RawMessage(`{"command":"rm -rf /"}`)},
	}
	e := newEnv(t, time.Hour, rec, fake.Step{Events: []harness.Event{permission, permission}})
	e.startLoop(t)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	evs := waitForEvents(t, rec, 1)
	if evs[0].Kind != EventNeedsHuman || evs[0].Priority != 4 {
		t.Fatalf("event = %+v", evs[0])
	}

	// Give the second approval time to be handled; it must not notify again.
	time.Sleep(200 * time.Millisecond)
	if got := len(rec.snapshot()); got != 1 {
		t.Fatalf("notifications = %d, want 1 per run", got)
	}
	if got, err := e.repos.Runs.Get(context.Background(), run.ID); err != nil || got.Outcome != domain.RunRunning {
		t.Fatalf("run should still be running: %+v (err %v)", got, err)
	}
}

func TestGetAndListResolveTheRunView(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, fake.Step{})
	e.startLoop(t)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	view, err := e.engine.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Session == nil || view.Session.ID != run.SessionID {
		t.Fatalf("session = %+v", view.Session)
	}
	if view.Template == nil || view.Template.ID != e.templateID {
		t.Fatalf("template = %+v", view.Template)
	}
	if view.Delivery != nil {
		t.Fatalf("delivery = %+v, want nil for a run with no delivery", view.Delivery)
	}

	list, err := e.engine.List(context.Background(), domain.RunFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Run.ID != run.ID {
		t.Fatalf("list = %+v", list)
	}

	none, err := e.engine.List(context.Background(), domain.RunFilter{Outcome: string(domain.RunSuccess)})
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("filtered list = %+v, want empty", none)
	}
}

func TestSummaryOfPrefersDiagnosisAndTruncates(t *testing.T) {
	long := make([]byte, 0, 300)
	for i := 0; i < 300; i++ {
		long = append(long, 'a')
	}
	report, err := json.Marshal(map[string]string{"diagnosis": string(long)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := summaryOf(report, "ignored"); len(got) != summaryLimit {
		t.Fatalf("summary length = %d, want %d", len(got), summaryLimit)
	}
	if got := summaryOf(json.RawMessage(`{"result":"x"}`), "  fallback  "); got != "fallback" {
		t.Fatalf("summary = %q", got)
	}
}
