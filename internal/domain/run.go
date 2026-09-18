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

	// StepRunID is set when this run is one attempt of a pipeline step
	// (origin "pipeline"), nil for every other run.
	StepRunID *string

	// LoopID is the loop this run is an iteration of ("" for an ordinary
	// one-shot run), and Iteration is its 1-based number within that loop
	// (0 when the run belongs to no loop).
	LoopID    string
	Iteration int

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
	LoopID    string
	Limit     int
}
