package domain

import "time"

// Profile configures how a session is run: allowed/disallowed tools, turn
// limits and whether approvals are required.
type Profile struct {
	ID              string
	Name            string
	Mode            string
	AllowedTools    []string
	DisallowedTools []string
	MaxTurns        int
	Unattended      bool
	ApprovalTimeout time.Duration
	Builtin         bool
	// Model is the default model for sessions started under this profile: an alias
	// ("fable", "opus", "sonnet", "haiku") or a full model name. Empty means the CLI's own
	// default.
	Model string
	// Effort is the default reasoning effort ("", low, medium, high, xhigh, max).
	Effort string
	// Harness names the agentic CLI sessions started under this profile run on
	// ("claude" | "codex"), unless the request names another. Empty is read as
	// "claude"; internal/db normalises it on write.
	Harness string
}
