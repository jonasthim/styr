// Package codex implements a harness.Harness that drives the OpenAI Codex CLI's
// `codex exec --json` protocol. This file contains only the codec: translating one stdout
// line into harness.Event values, plus the small amount of per-turn state the protocol forces
// on a decoder (the schema-constrained answer arrives as an ordinary final assistant message,
// not as a field on the completion event). See testdata/PROTOCOL.md for the recorded contract
// this codec was written against.
package codex

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

// envelope is the superset of top-level fields Styr reads across every observed line. Unknown
// fields are ignored by encoding/json, so additional CLI fields never break decoding.
type envelope struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     *item  `json:"item"`
	Usage    *usage `json:"usage"`
	// Message is the bare `error` envelope's payload; turn.failed nests the same text under
	// Error.Message instead (see testdata/PROTOCOL.md "turn.failed and the bare error envelope").
	Message string `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// usage is turn.completed's token report. The Codex stream has no cost and no duration field.
type usage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// item is the object carried by item.started / item.updated / item.completed. Its fields are a
// union across item types: only the ones belonging to item.Type are ever populated.
type item struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// agent_message
	Text string `json:"text"`
	// error
	Message string `json:"message"`
	// command_execution
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
	ExitCode         *int   `json:"exit_code"`
	Status           string `json:"status"`
	// file_change
	Changes []fileChange `json:"changes"`
}

// fileChange is one entry of a file_change item's `changes` array. No real line was ever
// recorded (see testdata/PROTOCOL.md), so every plausible path field is accepted and the entry
// is skipped when none of them is set.
type fileChange struct {
	Path     string `json:"path"`
	FilePath string `json:"file_path"`
	MovePath string `json:"move_path"`
	Kind     string `json:"kind"`
}

func (c fileChange) path() string {
	switch {
	case c.Path != "":
		return c.Path
	case c.FilePath != "":
		return c.FilePath
	default:
		return c.MovePath
	}
}

// DecodeLine converts one stdout line of `codex exec --json` into zero or more harness events.
// A non-JSON line produces one EventRaw with Err set. Lines the CLI emits but Styr does not act
// on (turn.started, item.updated, reasoning items) decode to no events at all; a truly
// unrecognised envelope or item type becomes EventRaw.
//
// model is echoed back on EventInit: the exec stream never names the model, so the only party
// that knows it is the caller that passed -m (empty means the CLI's own default).
//
// DecodeLine is stateless and therefore cannot fill Result.StructuredOutput, which the protocol
// delivers as the turn's final assistant message rather than as a field on turn.completed. Use
// Decoder for that.
func DecodeLine(line []byte, now time.Time, model string) []harness.Event {
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return []harness.Event{{Type: harness.EventRaw, At: now, Raw: cloneRaw(line), Err: err.Error()}}
	}

	switch env.Type {
	case "thread.started":
		// The thread id is reported twice on purpose: as SessionID, the
		// harness-side session identity every codec reports, and as
		// HarnessRef, the id a *later process* has to be started with to
		// continue this conversation (`codex exec resume <thread id>`). The
		// Claude codec fills only the former, because there the two would be
		// the same value — the Styr session id — and nothing has to be
		// carried across a restart.
		return []harness.Event{{Type: harness.EventInit, At: now, Init: &harness.Init{
			Harness: harness.KindCodex, SessionID: env.ThreadID, HarnessRef: env.ThreadID, Model: model,
		}}}

	case "turn.started":
		return nil

	case "turn.completed":
		r := &harness.Result{Subtype: "success", NumTurns: 1}
		if env.Usage != nil {
			r.InputTokens, r.OutputTokens = env.Usage.InputTokens, env.Usage.OutputTokens
		}
		return []harness.Event{{Type: harness.EventResult, At: now, Result: r}}

	case "turn.failed":
		msg := ""
		if env.Error != nil {
			msg = env.Error.Message
		}
		return []harness.Event{{Type: harness.EventResult, At: now, Result: &harness.Result{
			Subtype: "error", IsError: true, NumTurns: 1, Text: msg,
		}}}

	case "error":
		// The bare error envelope always precedes a turn.failed carrying the same text, which
		// is what becomes the turn's result; this line is kept only so the raw stream is
		// complete.
		return []harness.Event{{Type: harness.EventRaw, At: now, Raw: cloneRaw(line), Err: env.Message}}

	case "item.started", "item.completed":
		if env.Item == nil {
			return nil
		}
		return decodeItem(*env.Item, env.Type == "item.completed", now, line)

	case "item.updated":
		// Never observed in the recordings; a later snapshot of an item Styr has already
		// reported as started, so acting on it would duplicate events.
		return nil

	default:
		return []harness.Event{{Type: harness.EventRaw, At: now, Raw: cloneRaw(line)}}
	}
}

// decodeItem maps one item envelope. completed distinguishes item.completed (the terminal
// snapshot, which carries results) from item.started (the announcement, which carries inputs).
func decodeItem(it item, completed bool, now time.Time, line []byte) []harness.Event {
	switch it.Type {
	case "agent_message":
		if !completed {
			return nil // exec mode has no text deltas; only the completed message has text
		}
		return []harness.Event{{Type: harness.EventText, At: now, Text: it.Text}}

	case "reasoning":
		return nil // the Codex equivalent of a Claude thinking block

	case "command_execution":
		if !completed {
			input, _ := json.Marshal(map[string]string{"command": it.Command})
			return []harness.Event{{Type: harness.EventToolUse, At: now, ToolUse: &harness.ToolUse{
				ID: it.ID, Name: "Bash", Input: input,
			}}}
		}
		return []harness.Event{{Type: harness.EventToolResult, At: now, ToolResult: &harness.ToolResult{
			ToolUseID: it.ID, Content: it.AggregatedOutput, IsError: itemFailed(it),
		}}}

	case "file_change":
		if !completed {
			var out []harness.Event
			for i, ch := range it.Changes {
				p := ch.path()
				if p == "" {
					continue
				}
				input, _ := json.Marshal(map[string]string{"file_path": p, "kind": ch.Kind})
				out = append(out, harness.Event{Type: harness.EventToolUse, At: now, ToolUse: &harness.ToolUse{
					ID: changeID(it.ID, i, len(it.Changes)), Name: "Edit", Input: input,
				}})
			}
			return out
		}
		// The paths are repeated in the result because a file_change item may well arrive
		// without a preceding item.started (never observed either way), and the path is the
		// only part of the item worth showing.
		return []harness.Event{{Type: harness.EventToolResult, At: now, ToolResult: &harness.ToolResult{
			ToolUseID: it.ID, Content: changedPaths(it.Changes), IsError: itemFailed(it),
		}}}

	case "error":
		// Informational: the CLI reports these as items and the turn still completes.
		return []harness.Event{{Type: harness.EventRaw, At: now, Raw: cloneRaw(line), Err: it.Message}}

	default:
		return []harness.Event{{Type: harness.EventRaw, At: now, Raw: cloneRaw(line)}}
	}
}

// itemFailed reports whether a completed command_execution or file_change item ended badly.
func itemFailed(it item) bool {
	if it.Status == "failed" {
		return true
	}
	return it.ExitCode != nil && *it.ExitCode != 0
}

// changeID keeps a single-change file_change item addressable by the item id itself, and only
// suffixes when one item really does carry several paths.
func changeID(itemID string, i, n int) string {
	if n <= 1 {
		return itemID
	}
	return itemID + "#" + strconv.Itoa(i)
}

func changedPaths(changes []fileChange) string {
	out := ""
	for _, ch := range changes {
		p := ch.path()
		if p == "" {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += p
	}
	return out
}

func cloneRaw(line []byte) json.RawMessage { return append(json.RawMessage(nil), line...) }

// Decoder decodes one turn's stdout stream, carrying the per-turn state DecodeLine cannot:
// the schema-constrained answer is delivered as the turn's final agent_message rather than on
// the completion event, so the last assistant text has to be remembered until turn.completed
// arrives (see testdata/PROTOCOL.md "Structured output: an ordinary final message").
//
// A Decoder handles exactly one turn — that is one OS process, since `codex exec` runs one
// prompt per process — and must not be reused across turns.
type Decoder struct {
	model    string
	schema   bool
	lastText string
	// sawResult records whether the stream produced a terminal turn event, so the process
	// runner knows whether it has to synthesise one from the exit code.
	sawResult bool
}

// NewDecoder returns a Decoder for one turn. model is echoed on EventInit; schema says whether
// the turn was started with --output-schema, in which case the final assistant message is also
// reported as Result.StructuredOutput.
func NewDecoder(model string, schema bool) *Decoder {
	return &Decoder{model: model, schema: schema}
}

// Line decodes one stdout line, enriching the turn's result event with the final assistant
// text and, for a schema turn, with the structured output parsed from it.
func (d *Decoder) Line(line []byte, now time.Time) []harness.Event {
	evs := DecodeLine(line, now, d.model)
	for i := range evs {
		switch evs[i].Type {
		case harness.EventText:
			d.lastText = evs[i].Text
		case harness.EventResult:
			d.sawResult = true
			d.finish(evs[i].Result)
		}
	}
	return evs
}

// SawResult reports whether a terminal turn.completed or turn.failed line was decoded.
func (d *Decoder) SawResult() bool { return d.sawResult }

// finish fills in the parts of a Result that only the whole turn can supply.
func (d *Decoder) finish(r *harness.Result) {
	if r == nil {
		return
	}
	if r.Text == "" {
		r.Text = d.lastText
	}
	if !d.schema || d.lastText == "" {
		return
	}
	if raw := json.RawMessage(d.lastText); json.Valid(raw) {
		r.StructuredOutput = raw
	}
}
