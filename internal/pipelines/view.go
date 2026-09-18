package pipelines

import "github.com/jonasthim/styr/internal/domain"

// ValidationResult is POST /pipelines/validate's payload: whether the
// definition is valid, every problem found (empty when OK), and the graph
// rendering of it regardless of validity — so the UI can still draw a
// broken pipeline while pointing at what's wrong with it.
type ValidationResult struct {
	OK       bool
	Problems []Problem
	Graph    Graph
}

// StepView is one StepRun together with the Run started for its current
// attempt, when one has been (RunSummary is nil until the executor starts
// an attempt for this step).
type StepView struct {
	Step       domain.StepRun
	RunSummary *domain.Run
}

// RunView is a PipelineRun together with the records a UI needs to render
// it without further round trips: the Pipeline it belongs to, every step's
// StepView, and the pipeline's graph (so the UI can draw it without a
// second call to re-fetch or re-parse the pipeline's yaml).
type RunView struct {
	Run      domain.PipelineRun
	Pipeline domain.Pipeline
	Steps    []StepView
	Graph    Graph
}
