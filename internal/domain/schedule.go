package domain

import (
	"encoding/json"
	"time"
)

// Schedule is a cron-driven trigger that starts a template run on a
// cadence, ticked by internal/schedules every 30s. Unlike a webhook
// Trigger, a Schedule has no inbound payload: its vars are fixed at
// creation and merged with a `schedule` key (name, fired_at) on every run.
type Schedule struct {
	ID      string
	OwnerID *string // nil = shared, admin-created
	Name    string

	TemplateID string
	Cron       string
	Enabled    bool
	Vars       json.RawMessage

	LastRunAt   *time.Time
	LastOutcome string
	NextRunAt   *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ScheduleFiringStatus is the outcome of one scheduler tick for a
// schedule.
type ScheduleFiringStatus string

const (
	// FiringStarted means the tick started a run; RunID is set.
	FiringStarted ScheduleFiringStatus = "started"
	// FiringSkippedOverlap means the schedule's previous run was still
	// running, so this tick started nothing.
	FiringSkippedOverlap ScheduleFiringStatus = "skipped_overlap"
	// FiringFailed means the tick tried to start a run and could not;
	// Reason carries why.
	FiringFailed ScheduleFiringStatus = "failed"
)

// ScheduleFiring is one row in a schedule's firing history: whether a tick
// (or an explicit "run now") started a run, skipped it because the
// previous run was still going, or failed to start one.
type ScheduleFiring struct {
	ID         string
	ScheduleID string
	FiredAt    time.Time
	Status     ScheduleFiringStatus
	Reason     string
	RunID      *string
}

// ScheduleInput is the create/update payload for a Schedule. Enabled is a
// pointer so update can distinguish "leave enabled as-is" (nil) from an
// explicit true/false; create treats nil as enabled. Shared marks a
// schedule as owned by nobody (OwnerID nil) rather than the acting user;
// only an admin actor may set it.
type ScheduleInput struct {
	Name       string
	TemplateID string
	Cron       string
	Vars       json.RawMessage
	Enabled    *bool
	Shared     bool
}
