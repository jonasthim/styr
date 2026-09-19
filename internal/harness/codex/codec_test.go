package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

const fixtureThread = "01a0b707-ce58-7660-b301-4939ce14c766"

// decodeFixture runs every stdout line of a recorded fixture through a Decoder, skipping the
// recorder's trailing ">>> exit N" marker, and returns the events plus the decoder.
func decodeFixture(t *testing.T, name, model string, schema bool) ([]harness.Event, *Decoder) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dec := NewDecoder(model, schema)
	var out []harness.Event
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, ">>> ") {
			continue
		}
		out = append(out, dec.Line([]byte(line), time.Unix(0, 0))...)
	}
	return out, dec
}

// only returns the single event of the given type, failing when there is not exactly one.
func only(t *testing.T, evs []harness.Event, typ harness.EventType) harness.Event {
	t.Helper()
	var found []harness.Event
	for _, ev := range evs {
		if ev.Type == typ {
			found = append(found, ev)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one %s event, got %d (%v)", typ, len(found), types(evs))
	}
	return found[0]
}

func types(evs []harness.Event) []harness.EventType {
	out := make([]harness.EventType, len(evs))
	for i, ev := range evs {
		out[i] = ev.Type
	}
	return out
}

func TestDecodeSimpleText(t *testing.T) {
	evs, dec := decodeFixture(t, "01_simple_text.jsonl", "gpt-5.4-codex", false)

	init := only(t, evs, harness.EventInit).Init
	if init.Harness != harness.KindCodex {
		t.Errorf("init harness = %q, want codex", init.Harness)
	}
	if init.SessionID != fixtureThread {
		t.Errorf("init session id = %q, want the thread id %q", init.SessionID, fixtureThread)
	}
	// The exec stream never names the model, so the codec echoes what the caller passed as -m.
	if init.Model != "gpt-5.4-codex" {
		t.Errorf("init model = %q, want the model passed to the decoder", init.Model)
	}

	if got := only(t, evs, harness.EventText).Text; got != "pong" {
		t.Errorf("assistant text = %q, want pong", got)
	}

	r := only(t, evs, harness.EventResult).Result
	if r.IsError || r.Subtype != "success" {
		t.Errorf("result = %+v, want a successful turn", r)
	}
	if r.NumTurns != 1 {
		t.Errorf("result NumTurns = %d, want 1 (one process is one turn)", r.NumTurns)
	}
	if r.CostUSD != 0 {
		t.Errorf("result CostUSD = %v, want 0: the Codex stream reports no cost", r.CostUSD)
	}
	if r.InputTokens != 19234 || r.OutputTokens != 5 {
		t.Errorf("result tokens = %d/%d, want 19234/5", r.InputTokens, r.OutputTokens)
	}
	if r.Text != "pong" {
		t.Errorf("result text = %q, want the turn's final assistant message", r.Text)
	}
	if r.StructuredOutput != nil {
		t.Errorf("result StructuredOutput = %s, want nil without a schema", r.StructuredOutput)
	}
	if !dec.SawResult() {
		t.Error("SawResult() = false after a turn.completed line")
	}

	// The hook-warning error item is preserved verbatim rather than being turned into
	// assistant text or a failure.
	raw := only(t, evs, harness.EventRaw)
	if !strings.Contains(raw.Err, "clamping SessionEnd hook timeout") {
		t.Errorf("raw event Err = %q, want the error item's message", raw.Err)
	}
}

func TestDecodeCommandItemsBecomeBashToolEvents(t *testing.T) {
	evs, _ := decodeFixture(t, "02_read_file.jsonl", "", false)

	use := only(t, evs, harness.EventToolUse).ToolUse
	if use.Name != "Bash" {
		t.Errorf("tool name = %q, want Bash", use.Name)
	}
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(use.Input, &input); err != nil {
		t.Fatalf("tool input is not an object: %v", err)
	}
	if !strings.Contains(input.Command, "head -n 1 README.md") {
		t.Errorf("tool input command = %q, want the recorded shell command", input.Command)
	}

	res := only(t, evs, harness.EventToolResult).ToolResult
	if res.ToolUseID != use.ID {
		t.Errorf("tool result id = %q, want the tool use id %q", res.ToolUseID, use.ID)
	}
	if res.Content != "styr codex fixture workspace\n" {
		t.Errorf("tool result content = %q, want the command's aggregated output", res.Content)
	}
	if res.IsError {
		t.Error("tool result IsError = true for a command that exited 0")
	}
	if got := only(t, evs, harness.EventText).Text; got != "styr codex fixture workspace" {
		t.Errorf("assistant text = %q", got)
	}
}

func TestDecodeSchemaTurnFillsStructuredOutput(t *testing.T) {
	evs, _ := decodeFixture(t, "03_write_schema.jsonl", "", true)

	r := only(t, evs, harness.EventResult).Result
	if r.StructuredOutput == nil {
		t.Fatal("result StructuredOutput is nil for a --output-schema turn")
	}
	var got struct {
		Severity  string `json:"severity"`
		Diagnosis string `json:"diagnosis"`
	}
	if err := json.Unmarshal(r.StructuredOutput, &got); err != nil {
		t.Fatalf("structured output is not JSON: %v", err)
	}
	if got.Severity != "info" || got.Diagnosis == "" {
		t.Errorf("structured output = %+v, want the schema's severity and diagnosis", got)
	}
}

func TestDecodeSchemaTurnWithoutSchemaLeavesStructuredOutputNil(t *testing.T) {
	// The same fixture decoded as a plain turn: the JSON is only *interpreted* as structured
	// output because the caller asked for a schema, since the CLI delivers it as an ordinary
	// final assistant message either way.
	evs, _ := decodeFixture(t, "03_write_schema.jsonl", "", false)
	if r := only(t, evs, harness.EventResult).Result; r.StructuredOutput != nil {
		t.Errorf("StructuredOutput = %s, want nil when no schema was requested", r.StructuredOutput)
	}
}

func TestDecodeFailedTurn(t *testing.T) {
	evs, dec := decodeFixture(t, "03a_schema_rejected.jsonl", "", true)

	r := only(t, evs, harness.EventResult).Result
	if !r.IsError || r.Subtype != "error" {
		t.Errorf("result = %+v, want a failed turn", r)
	}
	if !strings.Contains(r.Text, "invalid_json_schema") {
		t.Errorf("result text = %q, want the API's rejection message", r.Text)
	}
	if r.StructuredOutput != nil {
		t.Errorf("StructuredOutput = %s, want nil when the turn never answered", r.StructuredOutput)
	}
	if !dec.SawResult() {
		t.Error("SawResult() = false after a turn.failed line")
	}
	// The bare `error` envelope that precedes turn.failed must not produce a second result.
	var raws int
	for _, ev := range evs {
		if ev.Type == harness.EventRaw && strings.Contains(ev.Err, "invalid_json_schema") {
			raws++
		}
	}
	if raws != 1 {
		t.Errorf("raw events carrying the API error = %d, want 1", raws)
	}
}

func TestDecodeResumeReportsTheSameThread(t *testing.T) {
	evs, _ := decodeFixture(t, "04_resume.jsonl", "", false)
	if got := only(t, evs, harness.EventInit).Init.SessionID; got != fixtureThread {
		t.Errorf("resumed session id = %q, want the original thread id %q", got, fixtureThread)
	}
	if got := only(t, evs, harness.EventText).Text; got != "pong" {
		t.Errorf("resumed assistant text = %q, want pong (the resumed turn kept its context)", got)
	}
}

func TestDecodeEmptyStreamHasNoResult(t *testing.T) {
	// A local failure prints nothing on stdout; the runner has to synthesise the result.
	evs, dec := decodeFixture(t, "05_resume_missing.jsonl", "", false)
	if len(evs) != 0 {
		t.Errorf("events = %v, want none", types(evs))
	}
	if dec.SawResult() {
		t.Error("SawResult() = true for a stream with no lines")
	}
}

// file_change items were never produced by a recorded session (the model wrote files with
// shell redirection), so this covers the shape documented in testdata/PROTOCOL.md.
func TestDecodeFileChangeItems(t *testing.T) {
	dec := NewDecoder("", false)
	started := `{"type":"item.started","item":{"id":"item_7","type":"file_change","changes":[{"path":"hello.txt","kind":"add"}],"status":"in_progress"}}`
	completed := `{"type":"item.completed","item":{"id":"item_7","type":"file_change","changes":[{"path":"hello.txt","kind":"add"}],"status":"completed"}}`

	evs := dec.Line([]byte(started), time.Unix(0, 0))
	if len(evs) != 1 || evs[0].Type != harness.EventToolUse {
		t.Fatalf("started events = %v, want one tool_use", types(evs))
	}
	use := evs[0].ToolUse
	if use.Name != "Edit" {
		t.Errorf("tool name = %q, want Edit", use.Name)
	}
	var input struct {
		FilePath string `json:"file_path"`
		Kind     string `json:"kind"`
	}
	if err := json.Unmarshal(use.Input, &input); err != nil {
		t.Fatalf("tool input: %v", err)
	}
	if input.FilePath != "hello.txt" || input.Kind != "add" {
		t.Errorf("tool input = %+v, want the changed path and kind", input)
	}

	evs = dec.Line([]byte(completed), time.Unix(0, 0))
	if len(evs) != 1 || evs[0].Type != harness.EventToolResult {
		t.Fatalf("completed events = %v, want one tool_result", types(evs))
	}
	if got := evs[0].ToolResult.Content; got != "hello.txt" {
		t.Errorf("tool result content = %q, want the changed path", got)
	}
	if evs[0].ToolResult.IsError {
		t.Error("tool result IsError = true for a completed change")
	}
}

func TestDecodeFileChangeWithSeveralPaths(t *testing.T) {
	dec := NewDecoder("", false)
	line := `{"type":"item.started","item":{"id":"item_7","type":"file_change","changes":[{"path":"a.txt","kind":"add"},{"move_path":"b.txt","kind":"update"},{"kind":"delete"}]}}`
	evs := dec.Line([]byte(line), time.Unix(0, 0))
	if len(evs) != 2 {
		t.Fatalf("events = %v, want one per change that names a path", types(evs))
	}
	if evs[0].ToolUse.ID == evs[1].ToolUse.ID {
		t.Errorf("both changes share tool use id %q; they must be distinguishable", evs[0].ToolUse.ID)
	}
}

func TestDecodeFailedCommandIsAnErrorResult(t *testing.T) {
	dec := NewDecoder("", false)
	line := `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"false","aggregated_output":"boom","exit_code":2,"status":"completed"}}`
	evs := dec.Line([]byte(line), time.Unix(0, 0))
	if len(evs) != 1 || evs[0].Type != harness.EventToolResult {
		t.Fatalf("events = %v, want one tool_result", types(evs))
	}
	if !evs[0].ToolResult.IsError {
		t.Error("tool result IsError = false for a command that exited 2")
	}
}

func TestDecodeIgnoredAndUnknownLines(t *testing.T) {
	for _, line := range []string{
		`{"type":"turn.started"}`,
		`{"type":"item.updated","item":{"id":"item_1","type":"command_execution"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"reasoning","text":"thinking"}}`,
		`{"type":"item.started","item":{"id":"item_1","type":"agent_message","text":""}}`,
	} {
		if evs := DecodeLine([]byte(line), time.Unix(0, 0), ""); len(evs) != 0 {
			t.Errorf("DecodeLine(%s) = %v, want no events", line, types(evs))
		}
	}

	for _, line := range []string{
		`{"type":"something.new","payload":1}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"web_search","query":"x"}}`,
	} {
		evs := DecodeLine([]byte(line), time.Unix(0, 0), "")
		if len(evs) != 1 || evs[0].Type != harness.EventRaw {
			t.Errorf("DecodeLine(%s) = %v, want one raw event", line, types(evs))
		}
		if string(evs[0].Raw) != line {
			t.Errorf("raw event kept %q, want the line verbatim", evs[0].Raw)
		}
	}

	evs := DecodeLine([]byte("not json"), time.Unix(0, 0), "")
	if len(evs) != 1 || evs[0].Type != harness.EventRaw || evs[0].Err == "" {
		t.Errorf("DecodeLine(not json) = %v, want one raw event with Err set", types(evs))
	}
}
