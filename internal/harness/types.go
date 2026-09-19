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

const (
	// KindClaude is the Harness kind for the Claude Code CLI.
	KindClaude Kind = "claude"
	// KindCodex is the Harness kind for the OpenAI Codex CLI (`codex exec`).
	KindCodex Kind = "codex"
)

// ErrUnsupported is returned by a Process method that the underlying CLI has no equivalent
// for. It is a normal outcome, not a failure: the Codex CLI, for instance, decides tool use
// with a sandbox policy chosen at process start and never asks the host, so its Decide always
// returns ErrUnsupported. Callers should test for it with errors.Is and degrade (hide the
// affordance) rather than reporting an error to the operator.
var ErrUnsupported = errors.New("harness: operation not supported by this harness")

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
	// ResumeRef is the *harness's own* id of the conversation to continue, as
	// a previous process reported it on Init.HarnessRef. It exists because
	// not every CLI resumes by the host's session id: Claude Code does (and
	// therefore ignores this field entirely, resuming by SessionID), while
	// the Codex CLI resumes by the thread id it minted itself, so its first
	// turn in a new process object runs `exec resume <ResumeRef>`. Empty
	// means "start a fresh harness-side conversation", which is also what a
	// resume with a ref the CLI no longer knows degrades to.
	ResumeRef string
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

	// Model, when non-empty, is passed as --model: either an alias ("fable", "opus",
	// "sonnet", "haiku") or a full model name. Empty leaves the CLI's own default.
	Model string
	// FallbackModel, when non-empty, is passed as --fallback-model: the model the CLI
	// falls back to when Model is unavailable (e.g. when it is rate limited).
	FallbackModel string
	// Effort, when non-empty, is passed as --effort: the reasoning effort level. One of
	// ValidEfforts.
	Effort string
}

// ValidEfforts are the reasoning-effort levels Styr accepts for StartSpec.Effort, in
// increasing order. The empty string means "leave the CLI's own default".
var ValidEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// ValidEffort reports whether effort is empty (the CLI default) or one of ValidEfforts.
func ValidEffort(effort string) bool {
	if effort == "" {
		return true
	}
	for _, e := range ValidEfforts {
		if e == effort {
			return true
		}
	}
	return false
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
	if !ValidEffort(s.Effort) {
		return fmt.Errorf("harness: effort %q is not allowed", s.Effort)
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
	// Harness names the CLI that reported this session in. Every codec sets it, so the UI can
	// show which harness a session is actually running under without consulting the session
	// row that asked for it.
	Harness Kind
	// HarnessRef is the harness's own id for this conversation, when the CLI
	// has one that is not the Styr session id: the Codex CLI's thread id,
	// which `codex exec resume <id>` takes. The sessions runner stores it on
	// the session row and hands it back as StartSpec.ResumeRef when it
	// reopens the session, so the CLI-side context survives a Styr resume.
	// Empty for a harness that resumes by the host's session id (Claude
	// Code), which is why the field is separate from SessionID rather than
	// being read off it.
	HarnessRef string
	SessionID  string
	Model      string
	Tools      []string
	// SlashCommands is the CLI's own `slash_commands` list from the init message: custom
	// project/user commands, plugin skills and the CLI's built-ins, all without the leading
	// slash (see internal/harness/claude/testdata/PROTOCOL.md).
	SlashCommands []string
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
	// Plan is the plan markdown the CLI is asking to exit plan mode with, filled in only when
	// ToolName is "ExitPlanMode" (observed as the request's input.plan field; see
	// internal/harness/claude/testdata/PROTOCOL.md "Plan mode in -p"). Empty for every other
	// tool.
	Plan string
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
