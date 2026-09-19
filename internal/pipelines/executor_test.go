package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
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
	"github.com/jonasthim/styr/internal/templates"
)

const (
	testSecret       = "0123456789abcdef0123456789abcdef" // 32+ bytes, for crypto.NewBox
	testWorkspace    = "ws-pipelines"
	testWorkspaceNam = "pipelines-ws"
	testReportSchema = `{"type":"object","properties":{"diagnosis":{"type":"string"}}}`
)

var admin = Actor{UserID: "u-admin", IsAdmin: true}

// base is the fixed instant the tests build their timelines around. Whole
// seconds only: stored timestamps are RFC3339Nano, which trims trailing
// zeros, so sub-second values do not order correctly as strings (and the
// timeout sweep compares them as strings).
var base = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

// mapLookup is a TemplateLookup over a name/id map.
type mapLookup map[string]string

func (m mapLookup) TemplateExists(name string) (string, bool) {
	id, ok := m[name]
	return id, ok
}

// mapByWorkspace is the test's TemplateLookupByWorkspace: one map per
// workspace id.
type mapByWorkspace map[string]mapLookup

func (m mapByWorkspace) ForWorkspace(_ context.Context, workspaceID string) (TemplateLookup, error) {
	return m[workspaceID], nil
}

// env is everything a pipeline-executor test needs to poke at the world the
// executor runs in: a real sessions service over the fake harness, a real
// run engine and a real executor over one temp database.
type env struct {
	exec    *Executor
	engine  *runs.Engine
	repos   Repos
	harness *fake.Harness
	bus     *events.Bus
	lookup  mapByWorkspace
	now     time.Time
}

// newEnv wires the whole stack and seeds three templates: "Triage" (which
// renders an alert and reports a diagnosis), "Review" (which renders a
// fan-out item) and "Broken" (whose prompt always renders empty, so
// starting its run fails).
func newEnv(t *testing.T, steps ...fake.Step) *env {
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

	ciphertext, nonce, err := box.Seal([]byte("sk-ant-service"))
	if err != nil {
		t.Fatalf("seal service token: %v", err)
	}
	if err := tokensRepo.SetService(ctx, ciphertext, nonce, "test"); err != nil {
		t.Fatalf("set service token: %v", err)
	}

	if err := workspacesRepo.Create(ctx, domain.Workspace{
		ID: testWorkspace, Name: testWorkspaceNam, Path: t.TempDir(), DefaultProfileID: "investigate",
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	lookup := mapByWorkspace{testWorkspace: mapLookup{}}
	for name, prompt := range map[string]string{
		"Triage": `Investigate {{ .alert }}.`,
		"Review": `Review {{ .item }}.`,
		"Fix":    `Apply this fix: {{ .plan }}`,
		"Broken": `{{ .nothing_at_all }}`,
	} {
		tpl := domain.Template{
			ID: uuid.NewString(), Name: name, WorkspaceID: testWorkspace, ProfileID: "investigate",
			TitleTemplate: name, PromptTemplate: prompt, ReportSchema: testReportSchema,
			CreatedAt: base, UpdatedAt: base,
		}
		if err := templatesRepo.Create(ctx, tpl); err != nil {
			t.Fatalf("create template %s: %v", name, err)
		}
		lookup[testWorkspace][name] = tpl.ID
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
		MaxOpen: 8, IdleTimeout: time.Hour, UsersDir: t.TempDir(), ServiceHome: t.TempDir(),
		Logger: slog.New(slog.DiscardHandler),
	})
	t.Cleanup(func() { _ = sessionsSvc.Shutdown(context.Background()) })

	engine := runs.New(runs.Repos{
		Runs:       db.NewRuns(database),
		Templates:  templatesRepo,
		Sessions:   sessionsRepo,
		Channels:   db.NewNotificationChannels(database),
		Deliveries: db.NewDeliveries(database),
		Events:     eventsRepo,
		Loops:      db.NewLoops(database),
	}, sessionsSvc, bus, nil, box, "https://styr.test/", time.Hour, slog.New(slog.DiscardHandler))

	e := &env{
		repos: Repos{
			Pipelines:    db.NewPipelines(database),
			PipelineRuns: db.NewPipelineRuns(database),
			StepRuns:     db.NewStepRuns(database),
			Runs:         db.NewRuns(database),
			Workspaces:   workspacesRepo,
		},
		engine: engine, harness: h, bus: bus, lookup: lookup, now: base,
	}
	e.exec = New(e.repos, engine, sessionsSvc, sessionsRepo, bus, lookup,
		func() time.Time { return e.now }, slog.New(slog.DiscardHandler))
	return e
}

// startLoops runs both the run engine's and the executor's bus loops for
// the duration of the test.
func (e *env) startLoops(t *testing.T) {
	t.Helper()
	for _, loop := range []func(context.Context){e.engine.Run, e.exec.Run} {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func(run func(context.Context)) {
			defer close(done)
			run(ctx)
		}(loop)
		t.Cleanup(func() {
			cancel()
			<-done
		})
	}
}

// pipeline creates a pipeline from yamlText, failing the test if it does
// not validate.
func (e *env) pipeline(t *testing.T, yamlText string) domain.Pipeline {
	t.Helper()
	pl, err := e.exec.CreatePipeline(context.Background(), admin, domain.PipelineInput{
		Name: "p-" + uuid.NewString()[:8], WorkspaceID: testWorkspace, YAML: yamlText, Shared: true,
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	return pl
}

// waitForPipeline polls until the pipeline run reaches want, or fails.
func (e *env) waitForPipeline(t *testing.T, id string, want domain.PipelineRunState) domain.PipelineRun {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		pr, err := e.repos.PipelineRuns.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get pipeline run: %v", err)
		}
		if pr.State == want {
			return *pr
		}
		if time.Now().After(deadline) {
			t.Fatalf("pipeline run %s: want state %s, still %s after 20s", id, want, pr.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForSteps polls until the pipeline run has at least n step runs.
func (e *env) waitForSteps(t *testing.T, id string, n int) []domain.StepRun {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		rows, err := e.repos.StepRuns.ListByPipelineRun(context.Background(), id)
		if err != nil {
			t.Fatalf("list step runs: %v", err)
		}
		if len(rows) >= n {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("pipeline run %s: want %d step runs, have %d after 20s", id, n, len(rows))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForStepState polls until stepID's newest attempt reaches want.
func (e *env) waitForStepState(t *testing.T, id, stepID string, want domain.StepRunState) domain.StepRun {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		rows, err := e.repos.StepRuns.ListByPipelineRun(context.Background(), id)
		if err != nil {
			t.Fatalf("list step runs: %v", err)
		}
		for _, sr := range currentAttempts(rows) {
			if sr.StepID == stepID && sr.State == want {
				return sr
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("step %s of run %s never reached %s", stepID, id, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sentTexts returns the first prompt each started session received, in the
// order the sessions were started.
func (e *env) sentTexts(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(e.harness.Procs))
	for _, p := range e.harness.Procs {
		if len(p.Sent) == 0 {
			out = append(out, "")
			continue
		}
		out = append(out, p.Sent[0].Text)
	}
	return out
}

func resultStep(res *harness.Result) fake.Step {
	return fake.Step{Events: []harness.Event{{Type: harness.EventResult, Result: res}}}
}

// reportStep scripts a session that ends with a structured report.
func reportStep(report string) fake.Step {
	return resultStep(&harness.Result{
		Subtype: "success", NumTurns: 1, CostUSD: 0.25, Text: "done",
		StructuredOutput: json.RawMessage(report),
	})
}

// A two-step sequential pipeline runs both steps in order and the second
// step's prompt carries the first step's diagnosis.
func TestTwoStepPipelineChainsTheFirstStepsReport(t *testing.T) {
	e := newEnv(t, reportStep(`{"diagnosis":"the linter is failing"}`))
	e.startLoops(t)

	pl := e.pipeline(t, `
name: fix-ci
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
    with:
      alert: "{{ .payload.title }}"
  - id: fix
    needs: [triage]
    template: Fix
    with:
      plan: "{{ .steps.triage.report.diagnosis }}"
`)

	pr, err := e.exec.Start(context.Background(), admin, pl.ID,
		templates.Vars{"payload": map[string]any{"title": "CI is red"}}, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pr.State != domain.PipelineRunRunning {
		t.Fatalf("state = %s, want running", pr.State)
	}

	finished := e.waitForPipeline(t, pr.ID, domain.PipelineRunSuccess)
	if finished.CostUSD != 0.5 {
		t.Fatalf("cost = %v, want the sum of both step runs (0.5)", finished.CostUSD)
	}

	sent := e.sentTexts(t)
	if len(sent) != 2 {
		t.Fatalf("started %d sessions, want 2: %q", len(sent), sent)
	}
	if sent[0] != "Investigate CI is red." {
		t.Fatalf("step 1 prompt = %q", sent[0])
	}
	if !strings.Contains(sent[1], "the linter is failing") {
		t.Fatalf("step 2 prompt = %q, want it to carry step 1's diagnosis", sent[1])
	}

	rows := e.waitForSteps(t, pr.ID, 2)
	for _, sr := range rows {
		if sr.State != domain.StepRunSuccess {
			t.Fatalf("step %s = %s, want success", sr.StepID, sr.State)
		}
		if sr.RunID == nil || sr.FinishedAt == nil {
			t.Fatalf("step %s has no run or no finished_at: %+v", sr.StepID, sr)
		}
	}

	// The run rows carry the step-run they belong to, and the origin the
	// plan documents.
	run, err := e.repos.Runs.Get(context.Background(), *rows[0].RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Origin != string(domain.OriginPipeline) {
		t.Fatalf("run origin = %q, want pipeline", run.Origin)
	}
	if run.StepRunID == nil || *run.StepRunID != rows[0].ID {
		t.Fatalf("run step_run_id = %v, want %s", run.StepRunID, rows[0].ID)
	}
}

// A `foreach` node expands into one step run per list item, each rendering
// its own `.item`.
func TestFanOutCreatesOneStepRunPerItem(t *testing.T) {
	e := newEnv(t, reportStep(`{"diagnosis":"two files to review"}`))
	e.startLoops(t)

	pl := e.pipeline(t, `
name: review-each
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
  - id: review
    needs: [triage]
    foreach: '["alpha","beta"]'
    template: Review
`)

	pr, err := e.exec.Start(context.Background(), admin, pl.ID, nil, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.waitForPipeline(t, pr.ID, domain.PipelineRunSuccess)

	rows, err := e.repos.StepRuns.ListByPipelineRun(context.Background(), pr.ID)
	if err != nil {
		t.Fatalf("list step runs: %v", err)
	}
	var items []string
	for _, sr := range rows {
		if sr.StepID == "review" {
			items = append(items, sr.Item)
		}
	}
	if len(items) != 2 {
		t.Fatalf("fan-out produced %d step runs, want 2 (%v)", len(items), items)
	}
	if items[0] != `"alpha"` || items[1] != `"beta"` {
		t.Fatalf("items = %v", items)
	}

	sent := e.sentTexts(t)
	if len(sent) != 3 {
		t.Fatalf("started %d sessions, want 3: %q", len(sent), sent)
	}
	if sent[1] != "Review alpha." || sent[2] != "Review beta." {
		t.Fatalf("fan-out prompts = %q, want each item rendered as .item", sent[1:])
	}
}

// A fan-out list longer than MaxFanout fails the step (and the run) rather
// than starting an unbounded number of sessions.
func TestFanOutOverTheLimitFailsTheStep(t *testing.T) {
	e := newEnv(t, reportStep(`{"diagnosis":"too many"}`))
	e.startLoops(t)

	items := make([]string, MaxFanout+1)
	for i := range items {
		items[i] = fmt.Sprintf("%q", fmt.Sprintf("f%d", i))
	}
	pl := e.pipeline(t, `
name: too-wide
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
  - id: review
    needs: [triage]
    foreach: '[`+strings.Join(items, ",")+`]'
    template: Review
`)

	pr, err := e.exec.Start(context.Background(), admin, pl.ID, nil, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.waitForPipeline(t, pr.ID, domain.PipelineRunFailed)

	sr := e.waitForStepState(t, pr.ID, "review", domain.StepRunFailed)
	if !strings.Contains(string(sr.Report), "maximum is 25") {
		t.Fatalf("report = %s, want the fan-out limit reason", sr.Report)
	}
	if got := len(e.harness.Procs); got != 1 {
		t.Fatalf("started %d sessions, want only the triage one", got)
	}
}

// A failing step retries within its budget and then fails the pipeline,
// skipping everything downstream.
func TestFailingStepRetriesOnceThenFailsThePipeline(t *testing.T) {
	e := newEnv(t, resultStep(&harness.Result{Subtype: "error", IsError: true, Text: "boom", CostUSD: 0.1}))
	e.startLoops(t)

	pl := e.pipeline(t, `
name: flaky
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
    retries: 1
  - id: review
    needs: [triage]
    template: Review
`)

	pr, err := e.exec.Start(context.Background(), admin, pl.ID, nil, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	finished := e.waitForPipeline(t, pr.ID, domain.PipelineRunFailed)
	if finished.CostUSD != 0.2 {
		t.Fatalf("cost = %v, want both attempts (0.2)", finished.CostUSD)
	}

	rows, err := e.repos.StepRuns.ListByPipelineRun(context.Background(), pr.ID)
	if err != nil {
		t.Fatalf("list step runs: %v", err)
	}
	attempts := map[int]domain.StepRunState{}
	var downstream domain.StepRun
	for _, sr := range rows {
		switch sr.StepID {
		case "triage":
			attempts[sr.Attempt] = sr.State
		case "review":
			downstream = sr
		}
	}
	if len(attempts) != 2 || attempts[1] != domain.StepRunFailed || attempts[2] != domain.StepRunFailed {
		t.Fatalf("triage attempts = %v, want two failed attempts", attempts)
	}
	if downstream.State != domain.StepRunSkipped {
		t.Fatalf("downstream step = %s, want skipped", downstream.State)
	}
	if got := len(e.harness.Procs); got != 2 {
		t.Fatalf("started %d sessions, want 2 (one per attempt)", got)
	}
}

// Cancel interrupts and closes the sessions of the steps still running.
func TestCancelClosesRunningStepSessions(t *testing.T) {
	// A step whose script emits nothing: the run never finishes on its own.
	e := newEnv(t, fake.Step{})
	e.startLoops(t)

	pl := e.pipeline(t, `
name: long
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
`)

	pr, err := e.exec.Start(context.Background(), admin, pl.ID, nil, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.waitForStepState(t, pr.ID, "triage", domain.StepRunRunning)

	if err := e.exec.Cancel(context.Background(), admin, pr.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	cancelled := e.waitForPipeline(t, pr.ID, domain.PipelineRunCancelled)
	if cancelled.FinishedAt == nil {
		t.Fatal("cancelled run has no finished_at")
	}
	e.waitForStepState(t, pr.ID, "triage", domain.StepRunCancelled)

	if len(e.harness.Procs) != 1 {
		t.Fatalf("started %d sessions, want 1", len(e.harness.Procs))
	}
	proc := e.harness.Procs[0]
	if proc.Interrupts == 0 || !proc.Closed {
		t.Fatalf("session interrupts = %d, closed = %v; want it interrupted and closed", proc.Interrupts, proc.Closed)
	}

	// Cancelling again is a conflict, not a second cancellation.
	if err := e.exec.Cancel(context.Background(), admin, pr.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Cancel error = %v, want ErrConflict", err)
	}
}

// Tick closes out a pipeline run that overran its definition's timeout.
func TestTickTimesOutAnOverrunningPipeline(t *testing.T) {
	e := newEnv(t, fake.Step{})
	e.startLoops(t)

	pl := e.pipeline(t, `
name: slow
workspace: pipelines-ws
timeout: 1s
steps:
  - id: triage
    template: Triage
`)

	pr, err := e.exec.Start(context.Background(), admin, pl.ID, nil, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.waitForStepState(t, pr.ID, "triage", domain.StepRunRunning)

	// Well inside the timeout: the sweep leaves the run alone.
	e.exec.Tick(context.Background(), base.Add(500*time.Millisecond))
	if got, _ := e.repos.PipelineRuns.Get(context.Background(), pr.ID); got.State != domain.PipelineRunRunning {
		t.Fatalf("state after an early tick = %s, want running", got.State)
	}

	e.exec.Tick(context.Background(), base.Add(2*time.Second))
	timedOut := e.waitForPipeline(t, pr.ID, domain.PipelineRunTimeout)
	if timedOut.FinishedAt == nil {
		t.Fatal("timed-out run has no finished_at")
	}
	e.waitForStepState(t, pr.ID, "triage", domain.StepRunCancelled)
	if proc := e.harness.Procs[0]; !proc.Closed {
		t.Fatal("timed-out step's session was not closed")
	}
}

// RetryFailed re-runs only the steps that failed; a step that already
// succeeded keeps its report and is not started again.
func TestRetryFailedReRunsOnlyTheFailedSteps(t *testing.T) {
	e := newEnv(t, reportStep(`{"diagnosis":"all good"}`))
	e.startLoops(t)

	// "Broken" renders an empty prompt, so starting its run always fails.
	pl := e.pipeline(t, `
name: half-broken
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
  - id: fix
    needs: [triage]
    template: Broken
`)

	pr, err := e.exec.Start(context.Background(), admin, pl.ID, nil, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.waitForPipeline(t, pr.ID, domain.PipelineRunFailed)
	e.waitForStepState(t, pr.ID, "triage", domain.StepRunSuccess)
	e.waitForStepState(t, pr.ID, "fix", domain.StepRunFailed)

	if err := e.exec.RetryFailed(context.Background(), admin, pr.ID); err != nil {
		t.Fatalf("RetryFailed: %v", err)
	}
	e.waitForPipeline(t, pr.ID, domain.PipelineRunFailed)

	rows, err := e.repos.StepRuns.ListByPipelineRun(context.Background(), pr.ID)
	if err != nil {
		t.Fatalf("list step runs: %v", err)
	}
	var triage, fix int
	for _, sr := range rows {
		switch sr.StepID {
		case "triage":
			triage++
			if sr.State != domain.StepRunSuccess {
				t.Fatalf("triage = %s, want its success preserved", sr.State)
			}
		case "fix":
			fix++
		}
	}
	if triage != 1 {
		t.Fatalf("triage step runs = %d, want the successful one untouched", triage)
	}
	if fix != 2 {
		t.Fatalf("fix step runs = %d, want a second attempt", fix)
	}
	if got := len(e.harness.Procs); got != 1 {
		t.Fatalf("started %d sessions, want only triage's (the retry never reaches a session)", got)
	}

	// Nothing left to retry now that every failed step has a fresh
	// (still failed) attempt is still a conflict-free retry; a run with no
	// failed steps at all is not.
	if err := e.exec.RetryFailed(context.Background(), admin, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("RetryFailed on a missing run = %v, want ErrNotFound", err)
	}
}

// An invalid definition never becomes a pipeline: create answers
// domain.ErrInvalid carrying every problem.
func TestCreatePipelineRejectsAnInvalidDefinition(t *testing.T) {
	e := newEnv(t)

	_, err := e.exec.CreatePipeline(context.Background(), admin, domain.PipelineInput{
		Name: "bad", WorkspaceID: testWorkspace, Shared: true, YAML: `
name: bad
workspace: pipelines-ws
steps:
  - id: one
    template: Nope
  - id: two
    needs: [three]
    template: Triage
`,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("CreatePipeline error = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), `template "Nope" not found`) {
		t.Fatalf("error = %v, want the unknown template problem", err)
	}
	if !strings.Contains(err.Error(), `needs unknown step "three"`) {
		t.Fatalf("error = %v, want the unknown need problem", err)
	}

	// Validate reports the same problems without failing.
	res, vErr := e.exec.Validate(context.Background(), admin, testWorkspace, []byte("steps:\n  - id: a\n    template: Nope\n"))
	if vErr != nil {
		t.Fatalf("Validate: %v", vErr)
	}
	if res.OK || len(res.Problems) == 0 {
		t.Fatalf("Validate = %+v, want problems", res)
	}
	if len(res.Graph.Nodes) != 1 {
		t.Fatalf("graph nodes = %+v, want the one step", res.Graph.Nodes)
	}
}

// GetRun answers with the pipeline, every step-run attempt and the graph.
func TestGetRunReturnsStepsAndGraph(t *testing.T) {
	e := newEnv(t, reportStep(`{"diagnosis":"ok"}`))
	e.startLoops(t)

	pl := e.pipeline(t, `
name: viewable
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
  - id: fix
    needs: [triage]
    template: Review
`)
	pr, err := e.exec.Start(context.Background(), admin, pl.ID, nil, domain.OriginUI, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	e.waitForPipeline(t, pr.ID, domain.PipelineRunSuccess)

	view, err := e.exec.GetRun(context.Background(), admin, pr.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if view.Pipeline.ID != pl.ID {
		t.Fatalf("view pipeline = %s, want %s", view.Pipeline.ID, pl.ID)
	}
	if len(view.Steps) != 2 {
		t.Fatalf("view steps = %d, want 2", len(view.Steps))
	}
	for _, s := range view.Steps {
		if s.RunSummary == nil {
			t.Fatalf("step %s has no run summary", s.Step.StepID)
		}
	}
	if len(view.Graph.Nodes) != 2 || len(view.Graph.Edges) != 1 {
		t.Fatalf("graph = %+v", view.Graph)
	}

	list, err := e.exec.ListRuns(context.Background(), admin, domain.PipelineRunFilter{PipelineID: pl.ID})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(list) != 1 || list[0].ID != pr.ID {
		t.Fatalf("ListRuns = %+v", list)
	}

	// A member who cannot see the (shared-but-admin-owned) pipeline's run
	// gets nothing back.
	stranger := Actor{UserID: "u-stranger"}
	if _, err := e.exec.GetRun(context.Background(), stranger, pr.ID); err != nil {
		t.Fatalf("GetRun as a member of a shared pipeline: %v", err)
	}
}

// A `worktree: shared` step continues in the worktree its dependency
// recorded, which is what RunInput.WorktreePath carries to sessions.
func TestSharedWorktreePicksTheFirstDependencysWorktree(t *testing.T) {
	current := []domain.StepRun{
		{StepID: "triage", Worktree: ""},
		{StepID: "fix", Worktree: "/ws/.styr/worktrees/s1"},
		{StepID: "other", Worktree: "/ws/.styr/worktrees/s2"},
	}
	if got := sharedWorktree(current, []string{"fix"}); got != "/ws/.styr/worktrees/s1" {
		t.Fatalf("sharedWorktree = %q", got)
	}
	if got := sharedWorktree(current, []string{"triage", "fix"}); got != "/ws/.styr/worktrees/s1" {
		t.Fatalf("sharedWorktree skipping a worktree-less dependency = %q", got)
	}
	if got := sharedWorktree(current, []string{"triage"}); got != "" {
		t.Fatalf("sharedWorktree with nothing to share = %q", got)
	}
}

// currentAttempts keeps the newest attempt of every fan-out slot.
func TestCurrentAttemptsKeepsTheNewestAttempt(t *testing.T) {
	rows := []domain.StepRun{
		{ID: "a1", StepID: "a", Attempt: 1, State: domain.StepRunFailed},
		{ID: "a2", StepID: "a", Attempt: 2, State: domain.StepRunSuccess},
		{ID: "b0", StepID: "b", IndexInFanout: 0, Attempt: 1, State: domain.StepRunPending},
		{ID: "b1", StepID: "b", IndexInFanout: 1, Attempt: 1, State: domain.StepRunRunning},
	}
	got := currentAttempts(rows)
	if len(got) != 3 {
		t.Fatalf("currentAttempts = %d rows, want 3", len(got))
	}
	if got[0].ID != "a2" || got[1].ID != "b0" || got[2].ID != "b1" {
		t.Fatalf("currentAttempts = %+v", got)
	}
	if stateOf(got, "a") != nodeSucceeded {
		t.Fatal("node a should count as succeeded through its retry")
	}
	if stateOf(got, "b") != nodeBusy || stateOf(got, "missing") != nodeAbsent {
		t.Fatal("node states are wrong")
	}
}

// decodeList reads both a JSON array and Go's own slice rendering.
func TestDecodeListAcceptsJSONAndGoSlices(t *testing.T) {
	items, err := decodeList(`["a","b"]`)
	if err != nil || len(items) != 2 || items[0] != "a" {
		t.Fatalf("decodeList(json) = %v, %v", items, err)
	}
	items, err = decodeList(`[main.go pkg/x.go]`)
	if err != nil || len(items) != 2 || items[1] != "pkg/x.go" {
		t.Fatalf("decodeList(go slice) = %v, %v", items, err)
	}
	if items, err := decodeList("   "); err != nil || items != nil {
		t.Fatalf("decodeList(empty) = %v, %v", items, err)
	}
	if _, err := decodeList("not a list"); err == nil {
		t.Fatal("decodeList should reject a non-list")
	}
}

// IsRunning is the narrow reader internal/schedules uses to decide whether
// a pipeline schedule's previous firing is still going. It follows the
// pipeline run's own state and reports ErrNotFound for a run that is gone -
// which the caller treats as "not overlapping".
func TestIsRunningFollowsThePipelineRunState(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, reportStep(`{"diagnosis":"all good"}`))

	pl := e.pipeline(t, `
name: one-step
workspace: pipelines-ws
steps:
  - id: triage
    template: Triage
`)
	pr := domain.PipelineRun{
		ID: "prun-is-running", PipelineID: pl.ID, Origin: string(domain.OriginSchedule),
		State: domain.PipelineRunRunning, StartedAt: e.now,
	}
	if err := e.repos.PipelineRuns.Create(ctx, pr); err != nil {
		t.Fatalf("create pipeline run: %v", err)
	}

	running, err := e.exec.IsRunning(ctx, pr.ID)
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !running {
		t.Fatalf("IsRunning on a running pipeline run = false, want true")
	}

	if err := e.repos.PipelineRuns.Finish(ctx, pr.ID, domain.PipelineRunSuccess, 0); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	running, err = e.exec.IsRunning(ctx, pr.ID)
	if err != nil {
		t.Fatalf("IsRunning after Finish: %v", err)
	}
	if running {
		t.Fatalf("IsRunning on a finished pipeline run = true, want false")
	}

	if _, err := e.exec.IsRunning(ctx, "no-such-run"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("IsRunning on a missing run = %v, want ErrNotFound", err)
	}
}
