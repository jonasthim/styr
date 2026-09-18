package domain

import "time"

// OriginLoop marks a run (or session) started as an iteration of a loop.
// It is accepted as a RunInput origin so a template carrying loop fields can
// be started by hand from the UI as a loop rather than as a webhook run.
const OriginLoop Origin = "loop"

// DefaultLoopUntilField is the report field a loop watches when its
// template names none.
const DefaultLoopUntilField = "done"

// LoopState is where a loop is in its lifecycle: it runs until the report
// field it watches turns truthy (done), until its iteration budget is spent
// (exhausted), until a run under it fails (failed), or until an operator
// stops it (stopped).
type LoopState string

const (
	LoopRunning   LoopState = "running"
	LoopDone      LoopState = "done"
	LoopExhausted LoopState = "exhausted"
	LoopFailed    LoopState = "failed"
	LoopStopped   LoopState = "stopped"
)

// Terminal reports whether s is an end state, i.e. no further iteration
// will be started under a loop in this state.
func (s LoopState) Terminal() bool { return s != LoopRunning }

// Loop is one until-done repetition of a template: every iteration is a
// run, and all of them share the loop's session, so each iteration sees the
// previous ones in the model's context. SessionID is nullable because a
// session can be deleted while its loop record stays as history.
type Loop struct {
	ID         string
	TemplateID string
	SessionID  *string

	// Origin (and OriginRef) is what started the loop — webhook, schedule
	// or ui — carried onto every run the loop creates so a loop's runs are
	// attributed to whatever asked for the work, not to the loop machinery.
	Origin    string
	OriginRef string

	// UntilField is the report field whose truthiness ends the loop;
	// MaxIterations is how many runs the loop may start in total.
	UntilField    string
	MaxIterations int

	// Iteration is the number of the run currently (or last) started,
	// counting from 1.
	Iteration int

	State     LoopState
	CreatedAt time.Time
	UpdatedAt time.Time
}
