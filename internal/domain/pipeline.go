package domain

import (
	"encoding/json"
	"time"
)

// Pipeline is a named, versioned YAML DAG of steps, each backed by a
// template run (see internal/pipelines for the YAML shape, parsing and
// validation). The yaml column is the source of truth; Definition-shaped
// fields are not duplicated onto this row.
type Pipeline struct {
	ID          string
	OwnerID     *string // nil = shared, admin-created
	Name        string
	WorkspaceID string
	YAML        string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// PipelineInput is the create/update payload for a Pipeline, shared by the
// service layer (which validates and persists it) and internal/api (which
// decodes it from a request body). Shared marks a pipeline as owned by
// nobody (OwnerID nil) rather than the acting user; only an admin actor may
// set it, a rule the service enforces.
type PipelineInput struct {
	Name        string
	WorkspaceID string
	YAML        string
	Shared      bool
}

// PipelineRunState is where a pipeline run is in its lifecycle.
type PipelineRunState string

const (
	PipelineRunRunning   PipelineRunState = "running"
	PipelineRunSuccess   PipelineRunState = "success"
	PipelineRunFailed    PipelineRunState = "failed"
	PipelineRunCancelled PipelineRunState = "cancelled"
	PipelineRunTimeout   PipelineRunState = "timeout"
)

// Terminal reports whether s is an end state, i.e. no further step run
// will be started under a pipeline run in this state.
func (s PipelineRunState) Terminal() bool { return s != PipelineRunRunning }

// PipelineRun is one execution of a Pipeline: one row per Start call, with
// one StepRun per node (fan-out creates N step-runs for one node).
type PipelineRun struct {
	ID         string
	PipelineID string

	// Origin (and OriginRef) is what started the run — ui, webhook or
	// schedule — carried onto every run each step-run starts.
	Origin    string
	OriginRef string

	// Input is the pipeline-level Vars payload (e.g. a trigger's normalised
	// webhook payload), available to every step's `with`/`foreach` as
	// .payload.
	Input json.RawMessage

	State      PipelineRunState
	StartedAt  time.Time
	FinishedAt *time.Time
	CostUSD    float64
}

// PipelineRunFilter narrows PipelineRuns.List's results.
type PipelineRunFilter struct {
	PipelineID string
	State      string
	Limit      int
}

// StepRunState is where one step-run is in its lifecycle.
type StepRunState string

const (
	StepRunPending   StepRunState = "pending"
	StepRunRunning   StepRunState = "running"
	StepRunSuccess   StepRunState = "success"
	StepRunFailed    StepRunState = "failed"
	StepRunSkipped   StepRunState = "skipped"
	StepRunCancelled StepRunState = "cancelled"
)

// Terminal reports whether s is an end state for a step-run.
func (s StepRunState) Terminal() bool {
	switch s {
	case StepRunSuccess, StepRunFailed, StepRunSkipped, StepRunCancelled:
		return true
	default:
		return false
	}
}

// StepRun is one node's execution within a PipelineRun. A `foreach` node
// gets one StepRun per fan-out item (IndexInFanout, Item); a retried
// attempt of the same node gets a new StepRun with the same StepID and
// IndexInFanout but an incremented Attempt, rather than mutating the
// failed one.
type StepRun struct {
	ID            string
	PipelineRunID string
	StepID        string // the step id from the yaml, e.g. "triage"
	IndexInFanout int    // 0 for a non-foreach step; the item's position for a foreach one
	Item          string // the foreach item's JSON, "" for a non-foreach step

	RunID   *string // nil until the run.Engine session for this attempt is started
	Attempt int

	State  StepRunState
	Report json.RawMessage

	StartedAt  *time.Time
	FinishedAt *time.Time

	// Worktree is the absolute path of the worktree this step ran in ("" =
	// none, or not yet started), carried forward to a `worktree: shared`
	// dependent.
	Worktree string
}
