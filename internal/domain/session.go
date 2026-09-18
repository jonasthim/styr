package domain

import "time"

// SessionState is where a session is in its lifecycle.
type SessionState string

const (
	SessionOpen    SessionState = "open"
	SessionRunning SessionState = "running"
	SessionWaiting SessionState = "waiting"
	SessionClosed  SessionState = "closed"
	SessionFailed  SessionState = "failed"
)

// Origin is what triggered a session's creation.
type Origin string

const (
	OriginUI       Origin = "ui"
	OriginWebhook  Origin = "webhook"
	OriginSchedule Origin = "schedule"
	OriginPipeline Origin = "pipeline"
)

// Session is one Claude Code run: its harness process, state and stats.
type Session struct {
	ID           string
	OwnerID      *string
	Title        string
	WorkspaceID  string
	ProfileID    string
	Harness      string
	State        SessionState
	Origin       Origin
	OriginRef    string
	Worktree     string
	CreatedAt    time.Time
	LastActiveAt time.Time
	NumTurns     int
	CostUSD      float64
	TokensIn     int
	TokensOut    int
	NowLine      string // latest "what it is doing" summary
	Model        string
}
