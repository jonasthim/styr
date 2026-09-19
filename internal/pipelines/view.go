package pipelines

import (
	"context"

	"github.com/jonasthim/styr/internal/domain"
)

// StepView is one step-run row together with the run it started, so the
// pipeline run page can show a step's outcome, cost and duration without a
// second round trip. RunSummary is nil for a step that never started a run
// (still pending, skipped behind a failure, or an empty fan-out marker).
type StepView struct {
	Step       domain.StepRun
	RunSummary *domain.Run
}

// RunView is everything GET /pipeline-runs/{id} answers: the run, the
// pipeline it belongs to, every step-run attempt in order, and the graph
// the UI draws them on. It lives here rather than in internal/domain
// because Graph is this package's type; internal/api imports it from here.
type RunView struct {
	Run      domain.PipelineRun
	Pipeline domain.Pipeline
	Steps    []StepView
	Graph    Graph
}

// GetRun returns one pipeline run with its pipeline, its step runs (every
// attempt, oldest first) and its graph. A step run whose run row cannot be
// read keeps a nil RunSummary rather than failing the whole view.
func (e *Executor) GetRun(ctx context.Context, actor Actor, id string) (RunView, error) {
	pr, err := e.repos.PipelineRuns.Get(ctx, id)
	if err != nil {
		return RunView{}, err
	}
	pl, err := e.GetPipeline(ctx, actor, pr.PipelineID)
	if err != nil {
		return RunView{}, err
	}

	view := RunView{Run: *pr, Pipeline: pl}
	if def, problems := Parse([]byte(pl.YAML)); len(problems) == 0 {
		view.Graph = def.Graph()
	}

	rows, err := e.repos.StepRuns.ListByPipelineRun(ctx, id)
	if err != nil {
		return RunView{}, err
	}
	view.Steps = make([]StepView, 0, len(rows))
	for _, sr := range rows {
		step := StepView{Step: sr}
		if sr.RunID != nil {
			if run, err := e.repos.Runs.Get(ctx, *sr.RunID); err == nil {
				step.RunSummary = run
			}
		}
		view.Steps = append(view.Steps, step)
	}
	return view, nil
}
