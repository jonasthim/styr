package codex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

// fakeBinary is the shell fake that replays a recorded fixture per invocation, modelling the
// real CLI's one-process-per-turn behaviour.
func fakeBinary(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "fake-codex", "fake-codex.sh"))
	if err != nil {
		t.Fatalf("resolve fake: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fake codex not found: %v", err)
	}
	return p
}

// startFake starts a session against the fake. argvLog, when non-empty, is the file the fake
// appends one line of argv to per invocation.
func startFake(t *testing.T, argvLog string, env map[string]string) harness.Process {
	t.Helper()
	dir := t.TempDir()
	spec := harness.StartSpec{
		SessionID: "0a4b2f26-3f5b-4c3a-9f6a-0f1d2c3b4a59",
		Cwd:       dir,
		Home:      dir,
		Profile:   harness.Profile{Mode: "default"},
		Env:       map[string]string{"FAKE_CODEX_DELAY": "0"},
	}
	for k, v := range env {
		spec.Env[k] = v
	}
	if argvLog != "" {
		spec.Env["FAKE_CODEX_ARGV_LOG"] = argvLog
	}
	p, err := New(fakeBinary(t)).Start(t.Context(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = p.Close(t.Context()) })
	return p
}

// turn sends a prompt and drains events until the turn's result, returning everything it saw.
func turn(t *testing.T, p harness.Process, prompt string) []harness.Event {
	t.Helper()
	if err := p.Send(t.Context(), harness.UserMessage{Text: prompt}); err != nil {
		t.Fatalf("Send(%q): %v", prompt, err)
	}
	var got []harness.Event
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				t.Fatalf("events channel closed mid-turn after %v", types(got))
			}
			got = append(got, ev)
			if ev.Type == harness.EventResult {
				return got
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the turn's result; saw %v", types(got))
		}
	}
}

func TestTwoTurnsAreTwoProcessesOnOneThread(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv.log")
	p := startFake(t, log, nil)

	first := turn(t, p, "[fixture:01] say pong")
	second := turn(t, p, "[fixture:04] and again")

	for i, evs := range [][]harness.Event{first, second} {
		if got := only(t, evs, harness.EventInit).Init.SessionID; got != fixtureThread {
			t.Errorf("turn %d reported thread %q, want %q", i+1, got, fixtureThread)
		}
		if r := only(t, evs, harness.EventResult).Result; r.IsError {
			t.Errorf("turn %d result = %+v, want success", i+1, r)
		}
	}

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("fake was invoked %d times, want 2 (one process per turn):\n%s", len(lines), b)
	}
	if strings.Contains(lines[0], "resume") {
		t.Errorf("first turn used resume: %s", lines[0])
	}
	if !strings.Contains(lines[1], "resume\t"+fixtureThread) {
		t.Errorf("second turn did not resume the thread learned from the first: %s", lines[1])
	}
	// The events channel survives across turns; only Close ends it.
	select {
	case ev, ok := <-p.Events():
		if !ok {
			t.Fatal("events channel closed between turns")
		}
		t.Fatalf("unexpected extra event between turns: %+v", ev)
	default:
	}
}

func TestSendWhileATurnIsRunningFails(t *testing.T) {
	p := startFake(t, "", map[string]string{"FAKE_CODEX_DELAY": "0.2"})
	if err := p.Send(t.Context(), harness.UserMessage{Text: "[fixture:01] one"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := p.Send(t.Context(), harness.UserMessage{Text: "[fixture:01] two"}); err == nil {
		t.Error("a second Send during a running turn returned no error; codex exec takes one prompt per process")
	}
}

func TestDecideIsUnsupported(t *testing.T) {
	p := startFake(t, "", nil)
	err := p.Decide(t.Context(), harness.Decision{RequestID: "r1", Allow: true})
	if !errors.Is(err, harness.ErrUnsupported) {
		t.Errorf("Decide error = %v, want harness.ErrUnsupported: Codex has no approval channel", err)
	}
}

func TestInterruptEndsTheTurn(t *testing.T) {
	p := startFake(t, "", map[string]string{"FAKE_CODEX_DELAY": "0.3"})
	if err := p.Send(t.Context(), harness.UserMessage{Text: "[fixture:02] read the file"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Wait for the turn to be under way before killing it.
	select {
	case ev := <-p.Events():
		if ev.Type != harness.EventInit {
			t.Fatalf("first event = %s, want init", ev.Type)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no event before the interrupt deadline")
	}
	if err := p.Interrupt(t.Context()); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				t.Fatal("events channel closed without a result")
			}
			if ev.Type != harness.EventResult {
				continue
			}
			if !ev.Result.IsError || ev.Result.Subtype != "interrupted" {
				t.Errorf("result = %+v, want an interrupted result", ev.Result)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for the interrupted result")
		}
	}
}

func TestInterruptWhenIdleDoesNothing(t *testing.T) {
	p := startFake(t, "", nil)
	if err := p.Interrupt(t.Context()); err != nil {
		t.Errorf("Interrupt on an idle session = %v, want nil", err)
	}
}

func TestCloseEmitsExitAndClosesTheChannel(t *testing.T) {
	p := startFake(t, "", nil)
	turn(t, p, "[fixture:01] say pong")

	if err := p.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ev, ok := <-p.Events()
	if !ok {
		t.Fatal("events channel closed without an exit event")
	}
	if ev.Type != harness.EventExit {
		t.Fatalf("last event = %s, want exit", ev.Type)
	}
	if ev.ExitCode != 0 {
		t.Errorf("exit code = %d, want the last turn's 0", ev.ExitCode)
	}
	if _, ok := <-p.Events(); ok {
		t.Error("events channel is still open after the exit event")
	}
	if err := p.Close(t.Context()); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	if err := p.Send(t.Context(), harness.UserMessage{Text: "again"}); err == nil {
		t.Error("Send after Close returned no error")
	}
}

func TestFailedTurnWithNoStdoutReportsStderr(t *testing.T) {
	p := startFake(t, "", nil)
	evs := turn(t, p, "[fixture:05] resume something gone")
	r := only(t, evs, harness.EventResult).Result
	if !r.IsError {
		t.Errorf("result = %+v, want a failure", r)
	}
	if !strings.Contains(r.Text, "no rollout found") {
		t.Errorf("result text = %q, want the CLI's stderr line", r.Text)
	}
}

func TestSchemaIsWrittenToAFileTheChildCanRead(t *testing.T) {
	log := filepath.Join(t.TempDir(), "argv.log")
	dir := t.TempDir()
	spec := harness.StartSpec{
		SessionID:  "0a4b2f26-3f5b-4c3a-9f6a-0f1d2c3b4a59",
		Cwd:        dir,
		Home:       dir,
		Profile:    harness.Profile{Mode: "default"},
		JSONSchema: `{"type":"object","additionalProperties":false,"required":["severity"],"properties":{"severity":{"type":"string"}}}`,
		Env:        map[string]string{"FAKE_CODEX_DELAY": "0", "FAKE_CODEX_ARGV_LOG": log},
	}
	p, err := New(fakeBinary(t)).Start(t.Context(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The fake fails the turn when --output-schema names a file it cannot read, so a
	// successful, structured turn proves the schema really reached the child as a file.
	evs := turn(t, p, "[fixture:03] write and report")
	r := only(t, evs, harness.EventResult).Result
	if r.IsError {
		t.Fatalf("result = %+v, want success (the schema file was unreadable?)", r)
	}
	if r.StructuredOutput == nil {
		t.Error("StructuredOutput is nil for a turn started with a schema")
	}

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	if !strings.Contains(string(b), "--output-schema") {
		t.Errorf("argv did not carry --output-schema:\n%s", b)
	}
	schemaPath := ""
	for _, f := range strings.Split(strings.TrimRight(string(b), "\n"), "\t") {
		if strings.HasPrefix(f, os.TempDir()) && strings.HasSuffix(f, ".json") {
			schemaPath = f
		}
	}
	if schemaPath == "" {
		t.Fatalf("no schema file path in argv:\n%s", b)
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(schemaPath); !os.IsNotExist(err) {
		t.Errorf("schema temp file %s still exists after Close (stat err %v)", schemaPath, err)
	}
}

func TestToolEventsReachTheCaller(t *testing.T) {
	p := startFake(t, "", nil)
	evs := turn(t, p, "[fixture:02] read the file")
	if use := only(t, evs, harness.EventToolUse).ToolUse; use.Name != "Bash" {
		t.Errorf("tool use name = %q, want Bash", use.Name)
	}
	if res := only(t, evs, harness.EventToolResult).ToolResult; res.Content == "" {
		t.Error("tool result content is empty")
	}
}

// Interrupting after a turn has already reported its result is a no-op, not an error: the
// child is finished and only waiting to be reaped, so there is nothing left to stop. The
// sessions service calls Interrupt on whatever process a session has, without knowing whether
// its last turn is still going.
func TestInterruptAfterATurnFinishedDoesNothing(t *testing.T) {
	p := startFake(t, "", nil)
	turn(t, p, "[fixture:01] say pong")

	if err := p.Interrupt(t.Context()); err != nil {
		t.Errorf("Interrupt after a finished turn = %v, want nil", err)
	}
	// And the session is still usable: the next turn runs normally.
	turn(t, p, "[fixture:01] again")
}
