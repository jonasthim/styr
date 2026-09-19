package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/runs"
	"github.com/jonasthim/styr/internal/templates"
)

// slot identifies one fan-out position of one node: a non-fan-out node has
// the single slot {id, 0}, a `foreach` node one slot per item.
type slot struct {
	stepID string
	index  int
}

// currentAttempts reduces every step run of a pipeline run to the newest
// attempt of each slot, in the order the slots first appear. A retried node
// keeps its failed attempts as history; only the newest attempt decides
// what the graph does next.
func currentAttempts(rows []domain.StepRun) []domain.StepRun {
	best := map[slot]int{}
	order := make([]slot, 0, len(rows))
	byslot := map[slot]domain.StepRun{}
	for _, sr := range rows {
		k := slot{stepID: sr.StepID, index: sr.IndexInFanout}
		if prev, ok := best[k]; !ok || sr.Attempt >= prev {
			if !ok {
				order = append(order, k)
			}
			best[k] = sr.Attempt
			byslot[k] = sr
		}
	}
	out := make([]domain.StepRun, 0, len(order))
	for _, k := range order {
		out = append(out, byslot[k])
	}
	return out
}

// nodeState is what the graph knows about one node from its current
// attempts.
type nodeState int

const (
	nodeAbsent    nodeState = iota // no step run yet (an unexpanded fan-out node)
	nodeBusy                       // at least one slot still pending or running
	nodeSucceeded                  // every slot succeeded
	nodeFailed                     // at least one slot failed, was skipped or cancelled
)

// stateOf summarises stepID from the pipeline run's current attempts.
func stateOf(current []domain.StepRun, stepID string) nodeState {
	state := nodeAbsent
	for _, sr := range current {
		if sr.StepID != stepID {
			continue
		}
		switch sr.State {
		case domain.StepRunFailed, domain.StepRunSkipped, domain.StepRunCancelled:
			return nodeFailed
		case domain.StepRunPending, domain.StepRunRunning:
			state = nodeBusy
		case domain.StepRunSuccess:
			if state != nodeBusy {
				state = nodeSucceeded
			}
		}
	}
	return state
}

// definitionOf parses the definition behind a pipeline run, reporting false
// when the pipeline row or its yaml can no longer be read (a definition
// edited into invalidity after the run started).
func (e *Executor) definitionOf(ctx context.Context, pr domain.PipelineRun) (domain.Pipeline, Definition, bool) {
	pl, err := e.repos.Pipelines.Get(ctx, pr.PipelineID)
	if err != nil {
		e.logger.Error("pipelines: load pipeline", "pipeline_run_id", pr.ID, "error", err)
		return domain.Pipeline{}, Definition{}, false
	}
	def, problems := Parse([]byte(pl.YAML))
	if len(problems) > 0 {
		e.logger.Error("pipelines: definition no longer parses", "pipeline_id", pl.ID, "problems", len(problems))
		return *pl, def, false
	}
	return *pl, def, true
}

// advanceLocked moves one pipeline run as far as it can: it expands the
// fan-out nodes whose dependencies just succeeded, starts every pending
// step whose dependencies all succeeded, and closes the run out when
// nothing is left to run. The caller holds e.mu.
func (e *Executor) advanceLocked(ctx context.Context, pipelineRunID string) {
	pr, err := e.repos.PipelineRuns.Get(ctx, pipelineRunID)
	if err != nil {
		e.logger.Error("pipelines: load pipeline run", "pipeline_run_id", pipelineRunID, "error", err)
		return
	}
	if pr.State.Terminal() {
		return
	}
	pl, def, ok := e.definitionOf(ctx, *pr)
	if !ok {
		e.finishRun(ctx, *pr, domain.PipelineRunFailed)
		return
	}
	lookup, err := e.lookupFor(ctx, pl.WorkspaceID)
	if err != nil {
		e.logger.Error("pipelines: template lookup", "pipeline_run_id", pipelineRunID, "error", err)
	}

	rows, err := e.repos.StepRuns.ListByPipelineRun(ctx, pipelineRunID)
	if err != nil {
		e.logger.Error("pipelines: list step runs", "pipeline_run_id", pipelineRunID, "error", err)
		return
	}
	current := currentAttempts(rows)

	for _, step := range def.Steps {
		if !depsSucceeded(current, step) {
			continue
		}
		if stateOf(current, step.ID) == nodeAbsent {
			if !e.expand(ctx, *pr, def, step, current) {
				continue
			}
			if rows, err = e.repos.StepRuns.ListByPipelineRun(ctx, pipelineRunID); err != nil {
				return
			}
			current = currentAttempts(rows)
		}
		for _, sr := range current {
			if sr.StepID != step.ID || sr.State != domain.StepRunPending {
				continue
			}
			e.startStep(ctx, *pr, def, step, sr, current, lookup)
		}
		if rows, err = e.repos.StepRuns.ListByPipelineRun(ctx, pipelineRunID); err != nil {
			return
		}
		current = currentAttempts(rows)
	}

	e.settle(ctx, *pr, current)
}

// depsSucceeded reports whether every node step needs has finished
// successfully (for a fan-out dependency, every one of its items).
func depsSucceeded(current []domain.StepRun, step Step) bool {
	for _, need := range step.Needs {
		if stateOf(current, need) != nodeSucceeded {
			return false
		}
	}
	return true
}

// settle closes a pipeline run out when nothing is left to do: failed when
// any node ended badly (its pending steps are skipped and anything still
// running is stopped), success when every node succeeded.
func (e *Executor) settle(ctx context.Context, pr domain.PipelineRun, current []domain.StepRun) {
	failed, busy := false, false
	for _, sr := range current {
		switch sr.State {
		case domain.StepRunFailed, domain.StepRunCancelled, domain.StepRunSkipped:
			failed = true
		case domain.StepRunPending, domain.StepRunRunning:
			busy = true
		}
	}
	switch {
	case failed:
		e.stopSteps(ctx, current, domain.StepRunSkipped, domain.StepRunCancelled)
		e.finishRun(ctx, pr, domain.PipelineRunFailed)
	case busy:
		return
	default:
		e.finishRun(ctx, pr, domain.PipelineRunSuccess)
	}
}

// stopSteps ends every step run that has not finished: a pending one is
// recorded as pendingState (skipped behind a failure, cancelled by hand or
// by the timeout sweep), and a running one has its session interrupted and
// closed first so it stops costing anything.
func (e *Executor) stopSteps(ctx context.Context, current []domain.StepRun,
	pendingState, runningState domain.StepRunState,
) {
	for _, sr := range current {
		switch sr.State {
		case domain.StepRunPending:
			e.setStepState(ctx, sr, pendingState, nil, "")
		case domain.StepRunRunning:
			// The step run is marked terminal before the session is
			// touched: interrupting makes the CLI emit a result, which
			// closes the run out and lands back here as a bus message, and
			// a terminal step run makes that a no-op.
			e.setStepState(ctx, sr, runningState, nil, "")
			e.closeStepSession(ctx, sr)
		}
	}
}

// closeStepSession interrupts and closes the session behind a step run.
func (e *Executor) closeStepSession(ctx context.Context, sr domain.StepRun) {
	if sr.RunID == nil || e.sessionsSvc == nil {
		return
	}
	run, err := e.repos.Runs.Get(ctx, *sr.RunID)
	if err != nil {
		return
	}
	if err := e.sessionsSvc.Interrupt(ctx, serviceActor, run.SessionID); err != nil {
		e.logger.Info("pipelines: interrupt step session", "step_run_id", sr.ID, "session_id", run.SessionID, "error", err)
	}
	if err := e.sessionsSvc.Close(ctx, serviceActor, run.SessionID); err != nil {
		e.logger.Error("pipelines: close step session", "step_run_id", sr.ID, "session_id", run.SessionID, "error", err)
	}
}

// setStepState records a step run's new state (and optional report),
// keeping the fields Update would otherwise overwrite, and announces the
// transition on the bus.
func (e *Executor) setStepState(ctx context.Context, sr domain.StepRun, state domain.StepRunState,
	report json.RawMessage, worktree string,
) {
	if worktree == "" {
		worktree = sr.Worktree
	}
	if report == nil {
		report = sr.Report
	}
	finishedAt := sr.FinishedAt
	if state.Terminal() && finishedAt == nil {
		at := e.now()
		finishedAt = &at
	}
	startedAt := sr.StartedAt
	if state == domain.StepRunPending {
		startedAt, finishedAt = nil, nil
	}
	if err := e.repos.StepRuns.Update(ctx, sr.ID, state, sr.RunID, report, worktree, startedAt, finishedAt); err != nil {
		e.logger.Error("pipelines: update step run", "step_run_id", sr.ID, "state", state, "error", err)
		return
	}
	e.publishState(sr.PipelineRunID, sr.ID, string(state))
}

// failStep records a step run that could not even be started (an unknown
// template, an unrenderable `with`, a `foreach` that is not a list) with
// the reason on its report, so the UI can show why.
func (e *Executor) failStep(ctx context.Context, sr domain.StepRun, reason string) {
	e.logger.Warn("pipelines: step failed", "step_run_id", sr.ID, "step_id", sr.StepID, "reason", reason)
	report, err := json.Marshal(map[string]string{"error": reason})
	if err != nil {
		report = json.RawMessage(`{"error":"step failed"}`)
	}
	e.setStepState(ctx, sr, domain.StepRunFailed, report, "")
}

// finishRun closes a pipeline run out with the summed cost of its step
// runs.
func (e *Executor) finishRun(ctx context.Context, pr domain.PipelineRun, state domain.PipelineRunState) {
	cost := e.costOf(ctx, pr.ID)
	if err := e.repos.PipelineRuns.Finish(ctx, pr.ID, state, cost); err != nil {
		e.logger.Error("pipelines: finish pipeline run", "pipeline_run_id", pr.ID, "state", state, "error", err)
		return
	}
	e.publishState(pr.ID, "", string(state))
	e.logger.Info("pipelines: finished", "pipeline_run_id", pr.ID, "state", state, "cost_usd", cost)
}

// costOf sums the cost of every run a pipeline run's steps started,
// attempts included: a retry really did cost twice.
func (e *Executor) costOf(ctx context.Context, pipelineRunID string) float64 {
	rows, err := e.repos.StepRuns.ListByPipelineRun(ctx, pipelineRunID)
	if err != nil {
		return 0
	}
	var total float64
	for _, sr := range rows {
		if sr.RunID == nil {
			continue
		}
		if run, err := e.repos.Runs.Get(ctx, *sr.RunID); err == nil {
			total += run.CostUSD
		}
	}
	return total
}

// publishState announces a pipeline-run or step-run transition on the
// event bus. stepRunID is empty for a pipeline-level transition.
func (e *Executor) publishState(pipelineRunID, stepRunID, state string) {
	if e.bus == nil {
		return
	}
	payload, err := json.Marshal(struct {
		PipelineRunID string `json:"pipeline_run_id"`
		StepRunID     string `json:"step_run_id,omitempty"`
		State         string `json:"state"`
	}{PipelineRunID: pipelineRunID, StepRunID: stepRunID, State: state})
	if err != nil {
		return
	}
	e.bus.Publish(events.Message{Kind: StateEventKind, Payload: payload})
}

// --- starting one step ---

// startStep renders the step's `with` values and starts the run behind one
// pending step run, recording the run and the worktree its session landed
// in. A step that cannot be started is failed rather than left pending.
func (e *Executor) startStep(ctx context.Context, pr domain.PipelineRun, def Definition, step Step,
	sr domain.StepRun, current []domain.StepRun, lookup TemplateLookup,
) {
	templateID := step.Template
	if lookup != nil {
		id, ok := lookup.TemplateExists(step.Template)
		if !ok {
			e.failStep(ctx, sr, fmt.Sprintf("template %q not found", step.Template))
			return
		}
		templateID = id
	}

	vars := e.varsFor(pr, def, current)
	if sr.Item != "" {
		vars["item"] = decodeItem(sr.Item)
	}
	for key, tmpl := range step.With {
		rendered, err := templates.Render(tmpl, vars)
		if err != nil {
			e.failStep(ctx, sr, fmt.Sprintf("with[%q]: %v", key, err))
			return
		}
		vars[key] = rendered
	}

	worktreePath := ""
	if step.Worktree == "shared" {
		worktreePath = sharedWorktree(current, step.Needs)
	}

	run, err := e.runs.Start(ctx, runs.RunInput{
		TemplateID:   templateID,
		Vars:         vars,
		Origin:       domain.OriginPipeline,
		StepRunID:    sr.ID,
		WorktreePath: worktreePath,
	})
	if err != nil {
		e.failStep(ctx, sr, "start run: "+err.Error())
		return
	}

	worktree := worktreePath
	if e.sessionsRepo != nil {
		if sess, err := e.sessionsRepo.Get(ctx, run.SessionID); err == nil && sess.Worktree != "" {
			worktree = sess.Worktree
		}
	}
	startedAt := e.now()
	runID := run.ID
	if err := e.repos.StepRuns.Update(ctx, sr.ID, domain.StepRunRunning, &runID, sr.Report, worktree, &startedAt, nil); err != nil {
		e.logger.Error("pipelines: mark step running", "step_run_id", sr.ID, "error", err)
		return
	}
	e.publishState(pr.ID, sr.ID, string(domain.StepRunRunning))
	e.logger.Info("pipelines: step started", "pipeline_run_id", pr.ID, "step_run_id", sr.ID,
		"step_id", sr.StepID, "attempt", sr.Attempt, "run_id", run.ID)
}

// sharedWorktree returns the worktree recorded for the first dependency
// that has one, which a `worktree: shared` step continues in.
func sharedWorktree(current []domain.StepRun, needs []string) string {
	for _, need := range needs {
		for _, sr := range current {
			if sr.StepID == need && sr.Worktree != "" {
				return sr.Worktree
			}
		}
	}
	return ""
}

// varsFor builds the variables every step of a pipeline run renders
// against: the run's own input, plus a `steps` map carrying each finished
// node's report (`steps.<id>.report`, or `steps.<id>.reports` for a
// fan-out node's list of them). The per-step `item` is added by the caller.
func (e *Executor) varsFor(pr domain.PipelineRun, def Definition, current []domain.StepRun) templates.Vars {
	vars := templates.Vars{}
	var input map[string]any
	if err := json.Unmarshal(pr.Input, &input); err == nil {
		for k, v := range input {
			vars[k] = v
		}
	}

	steps := map[string]any{}
	for _, step := range def.Steps {
		if step.Foreach != "" {
			reports := []any{}
			for _, sr := range current {
				if sr.StepID == step.ID && sr.State == domain.StepRunSuccess && sr.RunID != nil {
					reports = append(reports, decodeReport(sr.Report))
				}
			}
			steps[step.ID] = map[string]any{"reports": reports}
			continue
		}
		for _, sr := range current {
			if sr.StepID == step.ID && sr.State == domain.StepRunSuccess {
				steps[step.ID] = map[string]any{"report": decodeReport(sr.Report)}
			}
		}
	}
	vars["steps"] = steps
	return vars
}

// decodeReport turns a stored report into something a template can index
// into, falling back to an empty object.
func decodeReport(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

// decodeItem turns a stored fan-out item back into the value `.item`
// renders, falling back to its raw text.
func decodeItem(item string) any {
	var decoded any
	if err := json.Unmarshal([]byte(item), &decoded); err != nil {
		return item
	}
	return decoded
}

// --- fan-out ---

// expand creates the step runs of a node whose dependencies have just
// succeeded. A plain node already has its (pending) step run from Start, so
// only a `foreach` node ever reaches the list rendering below. It reports
// whether the node now has step runs to look at.
func (e *Executor) expand(ctx context.Context, pr domain.PipelineRun, def Definition, step Step, current []domain.StepRun) bool {
	if step.Foreach == "" {
		// A node whose pending step run is missing (a definition that
		// gained a step after the run started): create it now.
		if err := e.createStepRun(ctx, pr.ID, step.ID, 0, "", 1); err != nil {
			e.logger.Error("pipelines: create step run", "pipeline_run_id", pr.ID, "step_id", step.ID, "error", err)
			return false
		}
		return true
	}

	rendered, err := templates.Render(step.Foreach, e.varsFor(pr, def, current))
	if err != nil {
		e.createFailedStepRun(ctx, pr.ID, step.ID, fmt.Sprintf("foreach: %v", err))
		return true
	}
	items, err := decodeList(rendered)
	if err != nil {
		e.createFailedStepRun(ctx, pr.ID, step.ID, fmt.Sprintf("foreach: %v", err))
		return true
	}
	if len(items) > MaxFanout {
		e.createFailedStepRun(ctx, pr.ID, step.ID,
			fmt.Sprintf("foreach produced %d items, the maximum is %d", len(items), MaxFanout))
		return true
	}
	if len(items) == 0 {
		// Nothing to fan out over. The node still needs one terminal step
		// run, or its dependents could never become ready; it carries no
		// run, so it contributes no report.
		if err := e.repos.StepRuns.Create(ctx, domain.StepRun{
			ID:            uuid.NewString(),
			PipelineRunID: pr.ID,
			StepID:        step.ID,
			Attempt:       1,
			State:         domain.StepRunSuccess,
			Report:        json.RawMessage(`{}`),
		}); err != nil {
			e.logger.Error("pipelines: create empty fan-out marker", "step_id", step.ID, "error", err)
			return false
		}
		e.publishState(pr.ID, "", string(domain.StepRunSuccess))
		return true
	}

	for i, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			encoded = []byte(fmt.Sprintf("%q", fmt.Sprint(item)))
		}
		if err := e.createStepRun(ctx, pr.ID, step.ID, i, string(encoded), 1); err != nil {
			e.logger.Error("pipelines: create fan-out step run", "step_id", step.ID, "error", err)
			return false
		}
	}
	e.logger.Info("pipelines: fanned out", "pipeline_run_id", pr.ID, "step_id", step.ID, "items", len(items))
	return true
}

// createFailedStepRun records a node that could not be fanned out at all as
// one failed step run, so the failure is visible on the graph.
func (e *Executor) createFailedStepRun(ctx context.Context, pipelineRunID, stepID, reason string) {
	report, err := json.Marshal(map[string]string{"error": reason})
	if err != nil {
		report = json.RawMessage(`{"error":"fan-out failed"}`)
	}
	at := e.now()
	if err := e.repos.StepRuns.Create(ctx, domain.StepRun{
		ID:            uuid.NewString(),
		PipelineRunID: pipelineRunID,
		StepID:        stepID,
		Attempt:       1,
		State:         domain.StepRunFailed,
		Report:        report,
		FinishedAt:    &at,
	}); err != nil {
		e.logger.Error("pipelines: create failed step run", "step_id", stepID, "error", err)
		return
	}
	e.logger.Warn("pipelines: fan-out failed", "pipeline_run_id", pipelineRunID, "step_id", stepID, "reason", reason)
	e.publishState(pipelineRunID, "", string(domain.StepRunFailed))
}

// decodeList reads a rendered `foreach` value as a list: a JSON array
// (what `{{ json .steps.x.report.files }}` and a literal produce), or Go's
// own "[a b c]" rendering of a slice, which is what a bare
// `{{ .steps.x.report.files }}` prints.
func decodeList(rendered string) ([]any, error) {
	text := strings.TrimSpace(rendered)
	if text == "" {
		return nil, nil
	}
	var items []any
	if err := json.Unmarshal([]byte(text), &items); err == nil {
		return items, nil
	}
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		fields := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(text, "["), "]"))
		out := make([]any, 0, len(fields))
		for _, f := range fields {
			out = append(out, f)
		}
		return out, nil
	}
	return nil, fmt.Errorf("rendered %q, which is not a list", truncate(text, 80))
}

// truncate shortens s to at most n runes, for error messages built from
// rendered template output.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// --- completing one step ---

// completeStep applies a finished run to the step run it belongs to:
// success stores the report, a failure either opens a fresh attempt (within
// the step's retry budget) or ends the node. Either way the graph advances.
// The caller holds e.mu.
func (e *Executor) completeStep(ctx context.Context, runID string) {
	run, err := e.repos.Runs.Get(ctx, runID)
	if err != nil || run.StepRunID == nil {
		return
	}
	sr, err := e.repos.StepRuns.Get(ctx, *run.StepRunID)
	if err != nil {
		return
	}
	if sr.State.Terminal() {
		return // already cancelled, skipped or recorded
	}
	pr, err := e.repos.PipelineRuns.Get(ctx, sr.PipelineRunID)
	if err != nil {
		return
	}

	if run.Outcome == domain.RunSuccess {
		e.setStepState(ctx, *sr, domain.StepRunSuccess, run.Report, "")
		e.advanceLocked(ctx, pr.ID)
		return
	}

	e.setStepState(ctx, *sr, domain.StepRunFailed, run.Report, "")
	if !pr.State.Terminal() {
		if _, def, ok := e.definitionOf(ctx, *pr); ok {
			if step, found := stepByID(def, sr.StepID); found && sr.Attempt <= step.Retries {
				if err := e.createStepRun(ctx, pr.ID, sr.StepID, sr.IndexInFanout, sr.Item, sr.Attempt+1); err != nil {
					e.logger.Error("pipelines: create retry attempt", "step_run_id", sr.ID, "error", err)
				} else {
					e.logger.Info("pipelines: retrying step", "pipeline_run_id", pr.ID, "step_id", sr.StepID,
						"attempt", sr.Attempt+1, "of", step.Retries+1)
				}
			}
		}
	}
	e.advanceLocked(ctx, pr.ID)
}

// stepByID finds a step in a definition by its yaml id.
func stepByID(def Definition, id string) (Step, bool) {
	for _, s := range def.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}
