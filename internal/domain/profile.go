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
}
