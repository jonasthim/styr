package claude

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

func decodeFixture(t *testing.T, name string) []harness.Event {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var evs []harness.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if bytes.HasPrefix(sc.Bytes(), []byte(">>> ")) {
			continue // stdin echo, not CLI output
		}
		evs = append(evs, DecodeLine(sc.Bytes(), time.Unix(0, 0))...)
	}
	return evs
}

func count(evs []harness.Event, t harness.EventType) int {
	n := 0
	for _, e := range evs {
		if e.Type == t {
			n++
		}
	}
	return n
}

func TestSimpleTextFixture(t *testing.T) {
	evs := decodeFixture(t, "01_simple_text.jsonl")
	if evs[0].Type != harness.EventInit || evs[0].Init.SessionID == "" || evs[0].Init.Model == "" {
		t.Fatalf("first event = %+v, want init with session and model", evs[0])
	}
	if count(evs, harness.EventText) < 1 {
		t.Fatal("want at least one text event")
	}
	last := evs[len(evs)-1]
	if last.Type != harness.EventResult || last.Result.NumTurns < 1 || last.Result.IsError {
		t.Fatalf("last event = %+v, want successful result", last)
	}
	if count(evs, harness.EventRaw) != 0 {
		t.Fatalf("unexpected raw events: %d", count(evs, harness.EventRaw))
	}
}

func TestToolReadFixturePairsUseAndResult(t *testing.T) {
	evs := decodeFixture(t, "02_tool_read.jsonl")
	var useID string
	for _, e := range evs {
		if e.Type == harness.EventToolUse && e.ToolUse.Name == "Read" {
			useID = e.ToolUse.ID
		}
	}
	if useID == "" {
		t.Fatal("no Read tool_use")
	}
	found := false
	for _, e := range evs {
		if e.Type == harness.EventToolResult && e.ToolResult.ToolUseID == useID {
			found = true
		}
	}
	if !found {
		t.Fatal("tool_result does not reference tool_use id")
	}
}

func TestPermissionFixtureYieldsRequest(t *testing.T) {
	evs := decodeFixture(t, "03_permission_bash.jsonl")
	n := 0
	for _, e := range evs {
		if e.Type == harness.EventPermission {
			n++
			if e.Permission.RequestID == "" || e.Permission.ToolName != "Bash" || len(e.Permission.Input) == 0 {
				t.Fatalf("bad permission event %+v", e.Permission)
			}
		}
	}
	if n == 0 {
		t.Fatal("no permission_request decoded")
	}
}

func TestPartialsPresent(t *testing.T) {
	evs := decodeFixture(t, "01_simple_text.jsonl")
	if count(evs, harness.EventPartial) == 0 {
		t.Fatal("expected partial text deltas with --include-partial-messages")
	}
}

func TestGarbageLineBecomesRaw(t *testing.T) {
	evs := DecodeLine([]byte("not json"), time.Unix(0, 0))
	if len(evs) != 1 || evs[0].Type != harness.EventRaw || evs[0].Err == "" {
		t.Fatalf("got %+v", evs)
	}
}

func TestEncoders(t *testing.T) {
	u := string(EncodeUser("sid-1", "hello"))
	for _, want := range []string{`"type":"user"`, `"content":"hello"`, `"session_id":"sid-1"`} {
		if !strings.Contains(u, want) {
			t.Fatalf("user line %s lacks %s", u, want)
		}
	}
	d := string(EncodeDecision(harness.Decision{RequestID: "r1", Allow: false, Message: "no"}))
	for _, want := range []string{`"type":"control_response"`, `"request_id":"r1"`, `"behavior":"deny"`, `"message":"no"`} {
		if !strings.Contains(d, want) {
			t.Fatalf("decision line %s lacks %s", d, want)
		}
	}
	i := string(EncodeInterrupt("r2"))
	if !strings.Contains(i, `"subtype":"interrupt"`) || !strings.Contains(i, `"request_id":"r2"`) {
		t.Fatalf("interrupt line %s", i)
	}
	if !strings.HasSuffix(u, "\n") || !strings.HasSuffix(d, "\n") || !strings.HasSuffix(i, "\n") {
		t.Fatal("encoded lines must end with newline")
	}
}

func TestMultiTurnFixtureHasTwoResults(t *testing.T) {
	evs := decodeFixture(t, "04_multi_turn.jsonl")
	var results []harness.Event
	for _, e := range evs {
		if e.Type == harness.EventResult {
			results = append(results, e)
		}
	}
	if len(results) != 2 {
		t.Fatalf("got %d result events, want 2", len(results))
	}
	for i, r := range results {
		if r.Result.NumTurns < 1 {
			t.Fatalf("result %d: NumTurns = %d, want >= 1", i, r.Result.NumTurns)
		}
	}
	if got := results[len(results)-1].Result.Text; got != "41" {
		t.Fatalf("last result text = %q, want %q", got, "41")
	}
}

func TestInterruptFixture(t *testing.T) {
	evs := decodeFixture(t, "05_interrupt.jsonl")
	var lastResult *harness.Event
	for i := range evs {
		if evs[i].Type == harness.EventResult {
			lastResult = &evs[i]
		}
	}
	if lastResult == nil {
		t.Fatal("no result event decoded")
	}
	if !lastResult.Result.IsError {
		t.Fatalf("last result IsError = false, want true")
	}
	if lastResult.Result.Subtype != "error_during_execution" {
		t.Fatalf("last result Subtype = %q, want error_during_execution", lastResult.Result.Subtype)
	}
	// The control_response line (interrupt acknowledgement) must decode to no events.
	for _, e := range evs {
		if e.Type == harness.EventRaw {
			t.Fatalf("unexpected raw event: %+v", e)
		}
	}
}
