// Package harness defines the Harness/Process interface and event types used to drive an
// agentic CLI (Claude Code, and later others) without depending on any particular CLI's
// wire protocol. Concrete implementations (e.g. internal/harness/claude) exec the CLI and
// translate its protocol into these types.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Kind identifies which underlying CLI a Harness drives.
type Kind string

// KindClaude is the Harness kind for the Claude Code CLI.
const KindClaude Kind = "claude"

// Profile configures how a session is allowed to behave: which permission mode it runs
// under, which tools are explicitly allowed or disallowed, and a turn budget.
type Profile struct {
	Mode            string // "default" | "acceptEdits" | "plan" | "dontAsk" | "auto"; the CLI's skip-all-checks mode is never accepted
	AllowedTools    []string
	DisallowedTools []string
	MaxTurns        int
}

// StartSpec describes a session to start (or resume).
type StartSpec struct {
	SessionID string // UUID; required
	Resume    bool   // true: pass --resume SessionID instead of --session-id
	Title     string
	Cwd       string
	Home      string            // HOME for the child process
	Env       map[string]string // extra env, e.g. CLAUDE_CODE_OAUTH_TOKEN
	Profile   Profile
	Worktree  string // optional --worktree name

	// JSONSchema, when non-empty, is passed as --json-schema: the CLI constrains the model's
	// final turn to a synthetic StructuredOutput tool call matching this schema, and the
	// result's StructuredOutput field is populated (see testdata/PROTOCOL.md "structured
	// output").
	JSONSchema string
	// SystemPrompt, when non-empty, is passed as --append-system-prompt and appended to the
	// CLI's own system prompt. Not observable in the wire protocol; Styr never verifies it
	// took effect beyond the flag being accepted.
	SystemPrompt string
}

// validModes are the permission modes Styr is willing to pass to the CLI. Deliberately
// excludes the unattended "skip all permission checks" mode.
var validModes = map[string]bool{
	"default":     true,
	"acceptEdits": true,
	"plan":        true,
	"dontAsk":     true,
	"auto":        true,
}

// Validate returns an error when spec would produce an unsafe or unusable process.
func (s StartSpec) Validate() error {
	if _, err := uuid.Parse(s.SessionID); err != nil {
		return fmt.Errorf("harness: session id must be a UUID: %w", err)
	}
	if s.Cwd == "" || s.Home == "" {
		return errors.New("harness: cwd and home are required")
	}
	if !validModes[s.Profile.Mode] {
		return fmt.Errorf("harness: permission mode %q is not allowed", s.Profile.Mode)
	}
	if s.Profile.MaxTurns < 0 {
		return errors.New("harness: max turns must be >= 0")
	}
	return nil
}

// EventType identifies the kind of a decoded harness Event.
type EventType string

const (
	EventInit       EventType = "init"
	EventPartial    EventType = "partial" // text delta
	EventText       EventType = "text"    // complete assistant text block
	EventToolUse    EventType = "tool_use"
	EventToolResult EventType = "tool_result"
	EventPermission EventType = "permission_request"
	EventResult     EventType = "result"
	EventRaw        EventType = "raw"  // unknown message, kept verbatim
	EventExit       EventType = "exit" // process ended
	EventUser       EventType = "user" // a user turn recorded by the host; never produced by the CLI codec
)

// Init carries the session metadata reported when a session starts.
type Init struct {
	SessionID string
	Model     string
	Tools     []string
}

// ToolUse is a tool invocation requested by the model.
type ToolUse struct {
	ID              string
	Name            string
	Input           json.RawMessage
	ParentToolUseID string
}

// ToolResult is the outcome of a tool invocation.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// PermissionRequest asks the operator to allow or deny a pending tool use.
type PermissionRequest struct {
	RequestID string
	ToolName  string
	Input     json.RawMessage
	ToolUseID string
}

// Result is the final summary emitted at the end of a turn or session.
type Result struct {
	Subtype      string
	IsError      bool
	NumTurns     int
	CostUSD      float64
	InputTokens  int
	OutputTokens int
	DurationMS   int
	Text         string
	// StructuredOutput is the parsed report the model produced via a JSON-schema-constrained
	// turn (StartSpec.JSONSchema), observed on the CLI's result envelope as the top-level
	// "structured_output" object field. Nil when the session did not use a schema.
	StructuredOutput json.RawMessage
}

// Event is one decoded item from a harness Process's event stream.
type Event struct {
	Type       EventType
	At         time.Time
	Init       *Init
	Text       string
	ToolUse    *ToolUse
	ToolResult *ToolResult
	Permission *PermissionRequest
	Result     *Result
	Raw        json.RawMessage
	ExitCode   int
	Err        string
}

// UserMessage is a turn sent by the operator to a running session.
type UserMessage struct{ Text string }

// Decision answers a pending PermissionRequest.
type Decision struct {
	RequestID    string
	Allow        bool
	UpdatedInput json.RawMessage // optional; only with Allow
	Message      string          // shown to the model on deny
}

// Process is a running session driven by a Harness.
type Process interface {
	Events() <-chan Event
	Send(ctx context.Context, m UserMessage) error
	Decide(ctx context.Context, d Decision) error
	Interrupt(ctx context.Context) error
	Close(ctx context.Context) error // closes stdin, waits up to 10s, then kills
}

// Harness starts sessions for one underlying CLI.
type Harness interface {
	Kind() Kind
	Start(ctx context.Context, spec StartSpec) (Process, error)
}
