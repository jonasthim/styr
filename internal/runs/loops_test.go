package runs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
	"github.com/jonasthim/styr/internal/sessions"
)

// fixture 06 (internal/harness/claude/testdata/06_json_schema.jsonl) is a
// schema-constrained turn whose structured report carries severity,
// diagnosis and confidence — and no `done` field at all, which is exactly
// the "not done yet" case a loop has to keep iterating on.
const (
	unfinishedReport = `{"severity":"warning","diagnosis":"disk at 91%, cause not found yet","confidence":0.35}`
	finishedReport   = `{"severity":"ok","diagnosis":"journald rotated, disk back to 54%","confidence":0.9,"done":true}`
)

var loopOperator = sessions.Actor{UserID: "u-operator"}

// loopTemplate seeds a template that loops on the given report field, and
// returns its id.
func loopTemplate(t *testing.T, e *env, untilField string, max int) string {
	t.Helper()
	now := time.Now()
	tpl := domain.Template{
		ID: uuid.NewString(), Name: "Until fixed", WorkspaceID: testWorkspace, ProfileID: "investigate",
		TitleTemplate:  `{{ .status }}: until fixed`,
		PromptTemplate: `Investigate {{ .status }}.`,
		ReportSchema:   testReportSchema,
		LoopUntil:      untilField,
		LoopMax:        max,
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := e.repos.Templates.Create(context.Background(), tpl); err != nil {
		t.Fatalf("create looping template: %v", err)
	}
	return tpl.ID
}

// waitForLoop polls until the loop reaches want, or fails the test.
func waitForLoop(t *testing.T, e *env, id string, want domain.LoopState) domain.Loop {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		loop, err := e.repos.Loops.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get loop %s: %v", id, err)
		}
		if loop.State == want {
			return *loop
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s: want state %s, still %s", id, want, loop.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForSessionState polls until the session reaches want, or fails the
// test.
func waitForSessionState(t *testing.T, e *env, id string, want domain.SessionState) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		sess, err := e.repos.Sessions.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get session %s: %v", id, err)
		}
		if sess.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s: want state %s, still %s", id, want, sess.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func reportStep(report string) fake.Step {
	return resultStep(&harness.Result{
		Subtype: "success", NumTurns: 2, CostUSD: 0.1, Text: "reported",
		StructuredOutput: json.RawMessage(report),
	})
}

func TestLoopExhaustsAfterMaxIterationsOnOneSession(t *testing.T) {
	// Both turns report without a `done` field, so the loop never finishes
	// on its own and runs exactly its budget of two iterations.
	e := newEnv(t, time.Hour, &recorder{}, reportStep(unfinishedReport), reportStep(unfinishedReport))
	e.startLoop(t)
	templateID := loopTemplate(t, e, "done", 2)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.LoopID == "" || run.Iteration != 1 {
		t.Fatalf("first run = loop %q iteration %d, want a loop and iteration 1", run.LoopID, run.Iteration)
	}

	loop := waitForLoop(t, e, run.LoopID, domain.LoopExhausted)
	if loop.Iteration != 2 {
		t.Fatalf("iteration = %d, want 2", loop.Iteration)
	}
	if loop.UntilField != "done" || loop.MaxIterations != 2 {
		t.Fatalf("loop = %+v", loop)
	}
	if loop.SessionID == nil || *loop.SessionID != run.SessionID {
		t.Fatalf("session = %v, want the first run's session %q", loop.SessionID, run.SessionID)
	}
	if loop.Origin != string(domain.OriginWebhook) {
		t.Fatalf("origin = %q, want the run's origin", loop.Origin)
	}

	// Two runs, one session, one process: the loop resumes the same
	// conversation instead of starting a second one.
	chain, err := e.repos.Runs.ListByLoop(context.Background(), run.LoopID)
	if err != nil {
		t.Fatalf("ListByLoop: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("runs = %d, want 2", len(chain))
	}
	if chain[0].ID != run.ID || chain[1].Iteration != 2 {
		t.Fatalf("chain = %+v", chain)
	}
	if chain[1].SessionID != run.SessionID {
		t.Fatalf("second iteration ran on session %q, want %q", chain[1].SessionID, run.SessionID)
	}
	if chain[1].Origin != run.Origin {
		t.Fatalf("second iteration origin = %q, want the loop's %q", chain[1].Origin, run.Origin)
	}
	waitForRun(t, e.repos, chain[1].ID, domain.RunSuccess)

	if len(e.harness.Procs) != 1 {
		t.Fatalf("started %d processes, want 1", len(e.harness.Procs))
	}
	sent := e.harness.Procs[0].Sent
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2: %+v", len(sent), sent)
	}
	if sent[0].Text != "Investigate firing." {
		t.Fatalf("first message = %q", sent[0].Text)
	}
	if !strings.Contains(sent[1].Text, "Iteration 2 of 2") {
		t.Fatalf("second message = %q, want the iteration line", sent[1].Text)
	}
	if !strings.Contains(sent[1].Text, "Previous report") {
		t.Fatalf("second message = %q, want the previous report", sent[1].Text)
	}
	if !strings.Contains(sent[1].Text, "cause not found yet") {
		t.Fatalf("second message = %q, want the previous report's contents", sent[1].Text)
	}
	// The same variables render the prompt again.
	if !strings.Contains(sent[1].Text, "Investigate firing.") {
		t.Fatalf("second message = %q, want the template rendered with the original vars", sent[1].Text)
	}
}

func TestLoopFinishesWhenTheReportSaysDone(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, reportStep(finishedReport))
	e.startLoop(t)
	templateID := loopTemplate(t, e, "done", 3)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	loop := waitForLoop(t, e, run.LoopID, domain.LoopDone)
	if loop.Iteration != 1 {
		t.Fatalf("iteration = %d, want 1", loop.Iteration)
	}
	waitForRun(t, e.repos, run.ID, domain.RunSuccess)

	chain, err := e.repos.Runs.ListByLoop(context.Background(), run.LoopID)
	if err != nil {
		t.Fatalf("ListByLoop: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("runs = %d, want 1: a done report stops the loop", len(chain))
	}
	if got := e.harness.Procs[0].Sent; len(got) != 1 {
		t.Fatalf("sent %d messages, want 1", len(got))
	}
}

func TestLoopFailsWhenTheSessionFails(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, fake.Step{Events: []harness.Event{{Type: harness.EventExit, ExitCode: 2}}})
	e.startLoop(t)
	templateID := loopTemplate(t, e, "done", 3)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForRun(t, e.repos, run.ID, domain.RunFailed)
	loop := waitForLoop(t, e, run.LoopID, domain.LoopFailed)
	if loop.Iteration != 1 {
		t.Fatalf("iteration = %d, want 1", loop.Iteration)
	}
}

func TestStopEndsALoopAndClosesItsSession(t *testing.T) {
	// A step with no events leaves the first iteration running, so there is
	// a live loop to stop.
	e := newEnv(t, time.Hour, &recorder{}, fake.Step{})
	e.startLoop(t)
	templateID := loopTemplate(t, e, "done", 3)

	run, err := e.engine.Start(context.Background(), RunInput{TemplateID: templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx := context.Background()
	if err := e.engine.Stop(ctx, loopOperator, run.LoopID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	loop, err := e.repos.Loops.Get(ctx, run.LoopID)
	if err != nil {
		t.Fatalf("get loop: %v", err)
	}
	if loop.State != domain.LoopStopped {
		t.Fatalf("state = %s, want stopped", loop.State)
	}
	if len(e.harness.Procs) != 1 || !e.harness.Procs[0].Closed {
		t.Fatalf("want the loop's session process closed, procs = %+v", e.harness.Procs)
	}
	// Close ends the process; the session row reaches `closed` when the
	// harness's exit event comes back.
	waitForSessionState(t, e, run.SessionID, domain.SessionClosed)

	// Stopping again is a conflict: the loop is no longer running.
	if err := e.engine.Stop(ctx, loopOperator, run.LoopID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Stop: err = %v, want ErrConflict", err)
	}
	if err := e.engine.Stop(ctx, loopOperator, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Stop unknown: err = %v, want ErrNotFound", err)
	}
}

func TestGetLoopAndListLoops(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, reportStep(unfinishedReport), reportStep(unfinishedReport))
	e.startLoop(t)
	templateID := loopTemplate(t, e, "done", 2)

	ctx := context.Background()
	run, err := e.engine.Start(ctx, RunInput{TemplateID: templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForLoop(t, e, run.LoopID, domain.LoopExhausted)

	view, err := e.engine.GetLoop(ctx, run.LoopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if view.Loop.ID != run.LoopID || view.Loop.State != domain.LoopExhausted {
		t.Fatalf("loop = %+v", view.Loop)
	}
	if len(view.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(view.Runs))
	}
	if view.Runs[0].Iteration != 1 || view.Runs[1].Iteration != 2 {
		t.Fatalf("runs out of order: %+v", view.Runs)
	}
	if view.Runs[0].ID != run.ID {
		t.Fatalf("first run = %q, want %q", view.Runs[0].ID, run.ID)
	}
	if view.Template == nil || view.Template.ID != templateID {
		t.Fatalf("template = %+v", view.Template)
	}

	if _, err := e.engine.GetLoop(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetLoop unknown: err = %v, want ErrNotFound", err)
	}

	all, err := e.engine.ListLoops(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListLoops: %v", err)
	}
	if len(all) != 1 || all[0].ID != run.LoopID {
		t.Fatalf("loops = %+v", all)
	}
	exhausted, err := e.engine.ListLoops(ctx, string(domain.LoopExhausted), 10)
	if err != nil {
		t.Fatalf("ListLoops exhausted: %v", err)
	}
	if len(exhausted) != 1 {
		t.Fatalf("exhausted = %+v", exhausted)
	}
	running, err := e.engine.ListLoops(ctx, string(domain.LoopRunning), 10)
	if err != nil {
		t.Fatalf("ListLoops running: %v", err)
	}
	if len(running) != 0 {
		t.Fatalf("running = %+v, want none", running)
	}

	// The run view carries the loop, for the iteration chip on run detail.
	rv, err := e.engine.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rv.Loop == nil || rv.Loop.ID != run.LoopID {
		t.Fatalf("run view loop = %+v", rv.Loop)
	}
	if rv.Run.Iteration != 1 {
		t.Fatalf("iteration = %d, want 1", rv.Run.Iteration)
	}
	byLoop, err := e.engine.List(ctx, domain.RunFilter{LoopID: run.LoopID})
	if err != nil {
		t.Fatalf("List by loop: %v", err)
	}
	if len(byLoop) != 2 {
		t.Fatalf("runs by loop = %d, want 2", len(byLoop))
	}
}

func TestStartManualRunsATemplateAsUIOrigin(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, reportStep(finishedReport))
	e.startLoop(t)
	templateID := loopTemplate(t, e, "done", 2)

	ctx := context.Background()
	run, err := e.engine.StartManual(ctx, loopOperator, templateID, vars("firing"))
	if err != nil {
		t.Fatalf("StartManual: %v", err)
	}
	if run.Origin != string(domain.OriginUI) {
		t.Fatalf("origin = %q, want ui", run.Origin)
	}
	if run.LoopID == "" {
		t.Fatal("a looping template started by hand must still create a loop")
	}
	sess, err := e.repos.Sessions.Get(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.OwnerID != nil {
		t.Fatalf("owner = %v, want nil", *sess.OwnerID)
	}
	if sess.Origin != domain.OriginUI {
		t.Fatalf("session origin = %s, want ui", sess.Origin)
	}
	loop := waitForLoop(t, e, run.LoopID, domain.LoopDone)
	if loop.Origin != string(domain.OriginUI) {
		t.Fatalf("loop origin = %q, want ui", loop.Origin)
	}
}

func TestAnOrdinaryTemplateStartsNoLoop(t *testing.T) {
	e := newEnv(t, time.Hour, &recorder{}, reportStep(unfinishedReport))
	e.startLoop(t)

	ctx := context.Background()
	run, err := e.engine.Start(ctx, RunInput{TemplateID: e.templateID, Vars: vars("firing")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForRun(t, e.repos, run.ID, domain.RunSuccess)
	if run.LoopID != "" || run.Iteration != 0 {
		t.Fatalf("run = loop %q iteration %d, want neither", run.LoopID, run.Iteration)
	}
	loops, err := e.engine.ListLoops(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListLoops: %v", err)
	}
	if len(loops) != 0 {
		t.Fatalf("loops = %+v, want none", loops)
	}
	view, err := e.engine.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Loop != nil {
		t.Fatalf("run view loop = %+v, want nil", view.Loop)
	}
}

func TestLoopDoneTruthiness(t *testing.T) {
	cases := []struct {
		name   string
		report string
		field  string
		want   bool
	}{
		{"missing field", `{"severity":"warning","confidence":0.35}`, "done", false},
		{"false", `{"done":false}`, "done", false},
		{"zero", `{"done":0}`, "done", false},
		{"empty string", `{"done":""}`, "done", false},
		{"no", `{"done":"no"}`, "done", false},
		{"NO, padded", `{"done":"  NO  "}`, "done", false},
		{"false as a word", `{"done":"false"}`, "done", false},
		{"zero as a word", `{"done":"0"}`, "done", false},
		{"off", `{"done":"off"}`, "done", false},
		{"null", `{"done":null}`, "done", false},
		{"empty list", `{"done":[]}`, "done", false},
		{"empty object", `{"done":{}}`, "done", false},
		{"true", `{"done":true}`, "done", true},
		{"one", `{"done":1}`, "done", true},
		{"yes", `{"done":"yes"}`, "done", true},
		{"any other word", `{"done":"finished"}`, "done", true},
		{"non-empty list", `{"done":["x"]}`, "done", true},
		{"another field", `{"resolved":true}`, "resolved", true},
		{"field name empty falls back to done", `{"done":true}`, "", true},
		{"not an object", `"just text"`, "done", false},
		{"empty report", `{}`, "done", false},
		{"unparsable", `not json`, "done", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := loopDone(json.RawMessage(tc.report), tc.field); got != tc.want {
				t.Fatalf("loopDone(%s, %q) = %v, want %v", tc.report, tc.field, got, tc.want)
			}
		})
	}
}
