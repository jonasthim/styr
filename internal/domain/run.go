package domain

import (
	"encoding/json"
	"time"
)

// RunOutcome is where an unattended session's run is in its lifecycle.
type RunOutcome string

const (
	RunRunning    RunOutcome = "running"
	RunSuccess    RunOutcome = "success"
	RunFailed     RunOutcome = "failed"
	RunTimeout    RunOutcome = "timeout"
	RunNeedsHuman RunOutcome = "needs_human"
)

// Run is one unattended session started from a template, optionally tied
// back to the trigger and delivery that caused it.
type Run struct {
	ID         string
	SessionID  string
	TemplateID *string
	TriggerID  *string
	DeliveryID *string
	Origin     string

	StartedAt  time.Time
	FinishedAt *time.Time
	Outcome    RunOutcome

	Report  json.RawMessage
	Summary string
	CostUSD float64
}

// RunFilter narrows Runs.List's results.
type RunFilter struct {
	Outcome   string
	TriggerID string
	Limit     int
}
