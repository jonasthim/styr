package domain

import "time"

// Workspace is a working directory (optionally worktree-enabled) that
// sessions run in.
type Workspace struct {
	ID               string
	Name             string
	Path             string
	DefaultProfileID string
	Worktrees        bool
	CreatedAt        time.Time
}
