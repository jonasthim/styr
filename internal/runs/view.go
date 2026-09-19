package runs

import (
	"context"

	"github.com/jonasthim/styr/internal/domain"
)

// Get returns one run with its session, delivery and template resolved.
// Runs are visible to everyone (they are unattended, owned by no user), so
// Get takes no actor.
func (e *Engine) Get(ctx context.Context, id string) (domain.RunView, error) {
	run, err := e.repos.Runs.Get(ctx, id)
	if err != nil {
		return domain.RunView{}, err
	}
	return e.view(ctx, *run), nil
}

// List returns the runs matching f, newest first, each with its session,
// delivery and template resolved.
func (e *Engine) List(ctx context.Context, f domain.RunFilter) ([]domain.RunView, error) {
	rows, err := e.repos.Runs.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]domain.RunView, 0, len(rows))
	for _, r := range rows {
		out = append(out, e.view(ctx, r))
	}
	return out, nil
}

// view resolves the rows a run points at. A row that cannot be loaded (it
// was deleted, or the read failed) is left nil rather than failing the
// whole view: the run itself is the record that matters.
func (e *Engine) view(ctx context.Context, r domain.Run) domain.RunView {
	v := domain.RunView{Run: r}
	if sess, err := e.repos.Sessions.Get(ctx, r.SessionID); err == nil {
		v.Session = sess
	}
	if r.DeliveryID != nil && e.repos.Deliveries != nil {
		if dl, err := e.repos.Deliveries.Get(ctx, *r.DeliveryID); err == nil {
			v.Delivery = dl
		}
	}
	if r.TemplateID != nil {
		if tpl, err := e.repos.Templates.Get(ctx, *r.TemplateID); err == nil {
			v.Template = tpl
		}
	}
	if r.LoopID != "" && e.repos.Loops != nil {
		if loop, err := e.repos.Loops.Get(ctx, r.LoopID); err == nil {
			v.Loop = loop
		}
	}
	// A run a pipeline step started carries the step run's id; the step in
	// turn names the pipeline run, and that the pipeline. Each lookup is
	// best-effort like the ones above: the run is still worth showing
	// without the chip that says which graph it belongs to.
	if r.StepRunID != nil && e.repos.StepRuns != nil {
		if step, err := e.repos.StepRuns.Get(ctx, *r.StepRunID); err == nil {
			v.Step = step
			if e.repos.PipelineRuns != nil && e.repos.Pipelines != nil {
				if pr, err := e.repos.PipelineRuns.Get(ctx, step.PipelineRunID); err == nil {
					if pl, err := e.repos.Pipelines.Get(ctx, pr.PipelineID); err == nil {
						v.Pipeline = pl
					}
				}
			}
		}
	}
	return v
}
