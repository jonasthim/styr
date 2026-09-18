// Package claude implements a Harness that drives the Claude Code CLI's stream-json protocol.
// This file contains only the codec: translating CLI stdout lines into harness.Event values
// and encoding the few stdin lines Styr writes. See testdata/PROTOCOL.md for the recorded
// contract this codec was written against.
package claude

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

// envelope is the superset of top-level fields Styr reads across every observed message type.
// Unknown fields are ignored by encoding/json, so additional CLI fields never break decoding.
type envelope struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	SessionID string   `json:"session_id"`
	Model     string   `json:"model"`
	Tools     []string `json:"tools"`
	// SlashCommands is the init message's `slash_commands` array: every command the CLI
	// would accept as a `/name` user turn (custom commands, plugin skills, built-ins).
	SlashCommands []string        `json:"slash_commands"`
	Message       *apiMessage     `json:"message"`
	Event         *streamEvent    `json:"event"`
	RequestID     string          `json:"request_id"`
	Request       *controlRequest `json:"request"`
	// result fields
	IsError      bool    `json:"is_error"`
	NumTurns     int     `json:"num_turns"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	DurationMS   int     `json:"duration_ms"`
	Result       string  `json:"result"`
	Usage        *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	ParentToolUseID  string          `json:"parent_tool_use_id"`
}

// apiMessage is the "message" object shared by assistant and user envelopes. Content is left
// as raw JSON because its shape differs by message type: an array of content blocks for real
// assistant/tool_result messages, but a plain string for the CLI's echo of our own user turn
// (--replay-user-messages).
type apiMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type streamEvent struct {
	Type  string `json:"type"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type controlRequest struct {
	Subtype   string          `json:"subtype"`
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

// DecodeLine converts one stdout line from the Claude Code CLI into zero or more harness
// events. A non-JSON line produces one EventRaw with Err set. Message types the CLI emits but
// Styr does not act on (system subtypes other than init, rate_limit_event, control_response,
// stream_event deltas other than text_delta, the echoed user turn, thinking content blocks)
// decode to no events at all. Only a truly unrecognised top-level "type" becomes EventRaw.
func DecodeLine(line []byte, now time.Time) []harness.Event {
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return []harness.Event{{Type: harness.EventRaw, At: now, Raw: append([]byte(nil), line...), Err: err.Error()}}
	}

	switch env.Type {
	case "system":
		if env.Subtype != "init" {
			// hook_started, hook_response, hook_progress, status, commands_changed,
			// messages_changed, and any other non-init subtype: nothing Styr acts on.
			return nil
		}
		return []harness.Event{{Type: harness.EventInit, At: now, Init: &harness.Init{
			SessionID: env.SessionID, Model: env.Model, Tools: env.Tools, SlashCommands: env.SlashCommands,
		}}}

	case "rate_limit_event":
		return nil

	case "assistant":
		if env.Message == nil {
			return nil
		}
		blocks, err := decodeContentBlocks(env.Message.Content)
		if err != nil {
			return nil
		}
		var out []harness.Event
		for _, b := range blocks {
			switch b.Type {
			case "text":
				out = append(out, harness.Event{Type: harness.EventText, At: now, Text: b.Text})
			case "tool_use":
				out = append(out, harness.Event{Type: harness.EventToolUse, At: now, ToolUse: &harness.ToolUse{
					ID: b.ID, Name: b.Name, Input: b.Input, ParentToolUseID: env.ParentToolUseID,
				}})
				// "thinking" blocks (and any other block type) are ignored.
			}
		}
		return out

	case "user":
		if env.Message == nil {
			return nil
		}
		blocks, err := decodeContentBlocks(env.Message.Content)
		if err != nil {
			// message.content is a plain string: this is the CLI's echo of our own stdin
			// turn (--replay-user-messages). The service already has this text; no event.
			return nil
		}
		var out []harness.Event
		for _, b := range blocks {
			if b.Type == "tool_result" {
				out = append(out, harness.Event{Type: harness.EventToolResult, At: now, ToolResult: &harness.ToolResult{
					ToolUseID: b.ToolUseID, Content: flattenContent(b.Content), IsError: b.IsError,
				}})
			}
			// A "text" block here (e.g. the interruption notice) yields no event.
		}
		return out

	case "stream_event":
		if env.Event != nil && env.Event.Type == "content_block_delta" && env.Event.Delta != nil && env.Event.Delta.Type == "text_delta" {
			return []harness.Event{{Type: harness.EventPartial, At: now, Text: env.Event.Delta.Text}}
		}
		return nil

	case "result":
		r := &harness.Result{
			Subtype: env.Subtype, IsError: env.IsError, NumTurns: env.NumTurns,
			CostUSD: env.TotalCostUSD, DurationMS: env.DurationMS, Text: env.Result,
			StructuredOutput: env.StructuredOutput,
		}
		if env.Usage != nil {
			r.InputTokens, r.OutputTokens = env.Usage.InputTokens, env.Usage.OutputTokens
		}
		return []harness.Event{{Type: harness.EventResult, At: now, Result: r}}

	case "control_request":
		if env.Request != nil && env.Request.Subtype == "can_use_tool" {
			return []harness.Event{{Type: harness.EventPermission, At: now, Permission: &harness.PermissionRequest{
				RequestID: env.RequestID, ToolName: env.Request.ToolName, Input: env.Request.Input, ToolUseID: env.Request.ToolUseID,
			}}}
		}
		return nil

	case "control_response":
		// Echo of a control_response we wrote (interrupt ack, permission decision ack).
		return nil

	default:
		return []harness.Event{{Type: harness.EventRaw, At: now, Raw: append([]byte(nil), line...)}}
	}
}

// decodeContentBlocks unmarshals a message.content field that is expected to be an array of
// content blocks. It returns an error when content is not an array (e.g. a plain string),
// which callers use to detect the echoed-user-turn shape.
func decodeContentBlocks(raw json.RawMessage) ([]contentBlock, error) {
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// flattenContent turns a tool_result content (string or array of text blocks) into text.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			sb.WriteString(b.Text)
		}
		return sb.String()
	}
	return string(raw)
}

// EncodeUser returns the stdin line for a user turn.
func EncodeUser(sessionID, text string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": text},
		"parent_tool_use_id": nil,
		"session_id":         sessionID,
	})
	return append(b, '\n')
}

// EncodeDecision returns the control_response line answering a can_use_tool request.
func EncodeDecision(d harness.Decision) []byte {
	inner := map[string]any{}
	if d.Allow {
		inner["behavior"] = "allow"
		if len(d.UpdatedInput) > 0 {
			inner["updatedInput"] = json.RawMessage(d.UpdatedInput)
		}
	} else {
		inner["behavior"] = "deny"
		inner["message"] = d.Message
	}
	b, _ := json.Marshal(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": d.RequestID,
			"response":   inner,
		},
	})
	return append(b, '\n')
}

// EncodeInterrupt returns a control_request line with subtype interrupt and the given request id.
func EncodeInterrupt(requestID string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    map[string]any{"subtype": "interrupt"},
	})
	return append(b, '\n')
}
