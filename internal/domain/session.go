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
	ID          string
	OwnerID     *string
	Title       string
	WorkspaceID string
	ProfileID   string
	Harness     string
	// HarnessRef is the harness's own conversation id, as its init message
	// reported it (harness.Init.HarnessRef) — the Codex CLI's thread id.
	// It is what a resumed process is started with
	// (harness.StartSpec.ResumeRef) so the CLI-side conversation survives a
	// Styr resume or a model switch. Empty before the first process starts,
	// and always empty for a harness that resumes by the Styr session id
	// instead (Claude Code).
	HarnessRef string
	State      SessionState
	Origin     Origin
	OriginRef  string
	// Worktree is the absolute path of the git worktree this session runs
	// in, "" for a session that runs directly in the workspace checkout.
	Worktree string
	// Branch is the branch the worktree is checked out on, and BaseRef the
	// commit it started from; both empty without a worktree.
	Branch  string
	BaseRef string
	// WorktreeShared reports whether this session's worktree started life
	// as another session's (CreateInput.WorktreePath), rather than being
	// created fresh for it — a pipeline's "worktree: shared" step
	// continuing the previous step's work. It is informational only:
	// review and Discard identify a shared worktree by path, not this
	// flag.
	WorktreeShared bool
	CreatedAt      time.Time
	LastActiveAt   time.Time
	NumTurns       int
	CostUSD        float64
	TokensIn       int
	TokensOut      int
	NowLine        string // latest "what it is doing" summary
	Model          string
	// Effort is the reasoning effort the process was started with ("" = the CLI default).
	Effort string
	// SlashCommands is the command list the CLI reported on its last init message, without
	// the leading slash. Nil until a process has started.
	SlashCommands []string
	// DiffAdd and DiffDel are the worktree's line counts against BaseRef as
	// of the last turn, for the sessions list's diff badge.
	DiffAdd int
	DiffDel int
}
