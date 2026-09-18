package fake

import (
	"context"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

func testSpec() harness.StartSpec {
	return harness.StartSpec{
		SessionID: "0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10",
		Cwd:       "/tmp",
		Home:      "/tmp",
		Profile:   harness.Profile{Mode: "default"},
	}
}

func drain(t *testing.T, p harness.Process, timeout time.Duration) []harness.Event {
	t.Helper()
	var evs []harness.Event
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-time.After(50 * time.Millisecond):
			// no more events pending right now
			return evs
		case <-deadline:
			t.Fatal("timeout draining events")
		}
	}
}

// A Send replays exactly the step's events, then nothing more until the next Send.
func TestSendReplaysStepEventsThenNothing(t *testing.T) {
	step := Step{Events: []harness.Event{
		{Type: harness.EventText, Text: "hello"},
		{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
	}}
	h := New(step)
	p, err := h.Start(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send(context.Background(), harness.UserMessage{Text: "hi"}); err != nil {
		t.Fatal(err)
	}

	evs := drain(t, p, 2*time.Second)
	if len(evs) != 2 || evs[0].Type != harness.EventText || evs[1].Type != harness.EventResult {
		t.Fatalf("got %+v, want the step's two events", evs)
	}

	fp, ok := p.(*Process)
	if !ok {
		t.Fatalf("process type = %T, want *fake.Process", p)
	}
	if len(fp.Sent) != 1 || fp.Sent[0].Text != "hi" {
		t.Fatalf("Sent = %+v, want one message %q", fp.Sent, "hi")
	}

	// Nothing more arrives: no further steps are queued.
	more := drain(t, p, 200*time.Millisecond)
	if len(more) != 0 {
		t.Fatalf("unexpected extra events: %+v", more)
	}
}

// A permission step emits its first event (the permission request) and then blocks until Decide.
func TestPermissionStepBlocksUntilDecide(t *testing.T) {
	step := Step{
		WaitForDecision: true,
		Events: []harness.Event{
			{Type: harness.EventPermission, Permission: &harness.PermissionRequest{RequestID: "r1", ToolName: "Bash"}},
			{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
		},
	}
	h := New(step)
	p, err := h.Start(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send(context.Background(), harness.UserMessage{Text: "go"}); err != nil {
		t.Fatal(err)
	}

	select {
	case ev, ok := <-p.Events():
		if !ok || ev.Type != harness.EventPermission {
			t.Fatalf("got %+v, ok=%v, want a permission_request", ev, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for permission_request")
	}

	// The second event must not arrive until Decide is called.
	select {
	case ev, ok := <-p.Events():
		t.Fatalf("got event before Decide: %+v, ok=%v", ev, ok)
	case <-time.After(200 * time.Millisecond):
	}

	if err := p.Decide(context.Background(), harness.Decision{RequestID: "r1", Allow: true}); err != nil {
		t.Fatal(err)
	}

	select {
	case ev, ok := <-p.Events():
		if !ok || ev.Type != harness.EventResult {
			t.Fatalf("got %+v, ok=%v, want a result after Decide", ev, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result after Decide")
	}

	fp := p.(*Process)
	if len(fp.Decisions) != 1 || fp.Decisions[0].RequestID != "r1" || !fp.Decisions[0].Allow {
		t.Fatalf("Decisions = %+v", fp.Decisions)
	}
}

// Start keeps the spec's JSONSchema and SystemPrompt on Process.Spec so tests can assert
// pass-through, mirroring how internal/harness/claude.BuildArgs consumes them.
func TestStartKeepsJSONSchemaAndSystemPromptOnSpec(t *testing.T) {
	s := testSpec()
	s.JSONSchema = `{"type":"object"}`
	s.SystemPrompt = "You are terse."
	h := New()
	p, err := h.Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	fp := p.(*Process)
	if fp.Spec.JSONSchema != s.JSONSchema || fp.Spec.SystemPrompt != s.SystemPrompt {
		t.Fatalf("Spec = %+v, want JSONSchema=%q SystemPrompt=%q", fp.Spec, s.JSONSchema, s.SystemPrompt)
	}
}

// Close emits an exit event and closes the channel.
func TestCloseEmitsExitAndClosesChannel(t *testing.T) {
	h := New()
	p, err := h.Start(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	var sawExit bool
	for ev := range p.Events() {
		if ev.Type == harness.EventExit {
			sawExit = true
		}
	}
	if !sawExit {
		t.Fatal("expected an exit event before the channel closed")
	}

	fp := p.(*Process)
	if !fp.Closed {
		t.Fatal("Closed = false, want true")
	}

	// Idempotent: a second Close must not panic or block.
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
