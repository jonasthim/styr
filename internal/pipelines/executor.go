package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/runs"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/templates"
)

// Actor identifies who is calling the executor, for visibility checks.
// Shared with internal/sessions so the API layer passes one actor
// everywhere.
type Actor = sessions.Actor

// serviceActor is the actor the executor uses for the work no human asked
// for directly: closing a cancelled step's session, and the trigger /
// schedule paths that start a pipeline with no operator attached.
var serviceActor = Actor{IsAdmin: true}

// busBuffer is the executor's bus subscription depth. A dropped message
// would leave a step run stranded until the sweep notices, so the buffer is
// generous (the same reasoning as internal/runs).
const busBuffer = 512

// sweepInterval is how often Run sweeps for timed-out pipeline runs (and
// reconciles step runs whose bus message was dropped).
const sweepInterval = 30 * time.Second

// MaxFanout is the largest number of step runs one `foreach` node may
// expand to. A list longer than this fails the step rather than starting an
// unbounded number of sessions.
const MaxFanout = 25

// StateEventKind is the bus message kind published on every pipeline-run
// and step-run transition, forwarded to the UI by the SSE stream.
const StateEventKind = "pipeline.state"

// Repos bundles the repositories the executor reads and writes.
type Repos struct {
	Pipelines    *db.Pipelines
	PipelineRuns *db.PipelineRuns
	StepRuns     *db.StepRuns
	Runs         *db.Runs
	Workspaces   *db.Workspaces
}

// RunStarter starts one unattended run, the engine behind every step.
// *runs.Engine implements it; tests can substitute a recorder.
type RunStarter interface {
	Start(ctx context.Context, in runs.RunInput) (domain.Run, error)
}

// SessionCloser interrupts and closes the session behind a step's run, for
// Cancel and the timeout sweep. *sessions.Service implements it.
type SessionCloser interface {
	Interrupt(ctx context.Context, actor sessions.Actor, id string) error
	Close(ctx context.Context, actor sessions.Actor, id string) error
}

// TemplateLookupByWorkspace resolves the TemplateLookup a definition is
// validated and started against, for one workspace. NewTemplateIndex
// implements it over the templates repository; tests substitute a map.
type TemplateLookupByWorkspace interface {
	ForWorkspace(ctx context.Context, workspaceID string) (TemplateLookup, error)
}

// ValidationResult is the outcome of validating a definition: whether it is
// usable, every problem found (with yaml line numbers where known) and the
// graph the frontend draws.
type ValidationResult struct {
	OK       bool
	Problems []Problem
	Graph    Graph
}

// Executor owns pipelines, pipeline runs and step runs: it validates
// definitions, starts runs, and advances each run's graph as the runs
// behind its steps finish on the event bus.
type Executor struct {
	repos        Repos
	runs         RunStarter
	sessionsSvc  SessionCloser
	sessionsRepo *db.Sessions
	bus          *events.Bus
	templates    TemplateLookupByWorkspace
	now          func() time.Time
	logger       *slog.Logger

	// mu serialises every graph mutation. Advancing a run reads every step
	// run of the pipeline run and then writes several of them, so two
	// concurrent advances (a bus event arriving while Start is still
	// fanning the first level out) would otherwise both see the same
	// "pending" rows and start a step twice. Holding it across a
	// runs.Engine.Start is deliberate: it is also what guarantees that the
	// run.finished message for a step cannot be processed before the step
	// run row records the run it belongs to.
	mu sync.Mutex
}

// New constructs an Executor. sessionsSvc closes the sessions of cancelled
// and timed-out steps; sessionsRepo reads back the worktree a step's
// session landed in (carried forward to a `worktree: shared` dependent).
// now is the clock (nil means time.Now) and logger may be nil.
func New(repos Repos, runner RunStarter, sessionsSvc SessionCloser, sessionsRepo *db.Sessions,
	bus *events.Bus, tpl TemplateLookupByWorkspace, now func() time.Time, logger *slog.Logger,
) *Executor {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		repos: repos, runs: runner, sessionsSvc: sessionsSvc, sessionsRepo: sessionsRepo,
		bus: bus, templates: tpl, now: now, logger: logger,
	}
}

// --- template lookup over the repository ---

// templateIndex is a TemplateLookup over one workspace's templates,
// matching a step's `template` field by name or by id (the plan documents
// both).
type templateIndex struct {
	byName map[string]string
	byID   map[string]string
}

func (t templateIndex) TemplateExists(name string) (string, bool) {
	if id, ok := t.byID[name]; ok {
		return id, true
	}
	id, ok := t.byName[name]
	return id, ok
}

// repoTemplates resolves a workspace's templates through the templates
// repository. Templates are matched regardless of owner: a pipeline step is
// unattended and runs with the service token, exactly like a trigger's.
type repoTemplates struct{ repo *db.Templates }

// NewTemplateIndex returns the TemplateLookupByWorkspace the composition
// root wires into New.
func NewTemplateIndex(repo *db.Templates) TemplateLookupByWorkspace { return repoTemplates{repo: repo} }

func (r repoTemplates) ForWorkspace(ctx context.Context, workspaceID string) (TemplateLookup, error) {
	rows, err := r.repo.ListVisible(ctx, "", true)
	if err != nil {
		return nil, err
	}
	idx := templateIndex{byName: map[string]string{}, byID: map[string]string{}}
	for _, tpl := range rows {
		if tpl.WorkspaceID != workspaceID {
			continue
		}
		idx.byName[tpl.Name] = tpl.ID
		idx.byID[tpl.ID] = tpl.ID
	}
	return idx, nil
}

// --- visibility ---

// visibleOwner reports whether actor may see a row owned by owner: admins
// see everything, and everyone sees shared (owner-less) rows. Same rule as
// templates, triggers and schedules.
func visibleOwner(actor Actor, owner *string) bool {
	return actor.IsAdmin || owner == nil || *owner == actor.UserID
}

// mutableBy reports whether actor may change a row owned by owner: its
// owner, or an admin. A shared row is admin-only.
func mutableBy(actor Actor, owner *string) bool {
	return actor.IsAdmin || (owner != nil && *owner == actor.UserID)
}

// ownerFor returns the owner a newly created row gets: nil (shared) only
// when an admin explicitly asked for it, otherwise the actor.
func ownerFor(actor Actor, shared bool) *string {
	if actor.IsAdmin && shared {
		return nil
	}
	id := actor.UserID
	return &id
}

// --- CRUD ---

// CreatePipeline validates in.YAML against in.WorkspaceID's templates and
// stores it. An invalid definition is domain.ErrInvalid carrying every
// problem, so the API can answer 422 with the list.
func (e *Executor) CreatePipeline(ctx context.Context, actor Actor, in domain.PipelineInput) (domain.Pipeline, error) {
	if strings.TrimSpace(in.Name) == "" {
		return domain.Pipeline{}, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	if in.WorkspaceID == "" {
		return domain.Pipeline{}, fmt.Errorf("%w: workspace_id is required", domain.ErrInvalid)
	}
	res, err := e.Validate(ctx, actor, in.WorkspaceID, []byte(in.YAML))
	if err != nil {
		return domain.Pipeline{}, err
	}
	if !res.OK {
		return domain.Pipeline{}, problemsError(res.Problems)
	}

	now := e.now()
	pl := domain.Pipeline{
		ID:          uuid.NewString(),
		OwnerID:     ownerFor(actor, in.Shared),
		Name:        strings.TrimSpace(in.Name),
		WorkspaceID: in.WorkspaceID,
		YAML:        in.YAML,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := e.repos.Pipelines.Create(ctx, pl); err != nil {
		return domain.Pipeline{}, wrapConstraint(err)
	}
	return pl, nil
}

// ListPipelines returns every pipeline visible to actor.
func (e *Executor) ListPipelines(ctx context.Context, actor Actor) ([]domain.Pipeline, error) {
	return e.repos.Pipelines.ListVisible(ctx, actor.UserID, actor.IsAdmin)
}

// GetPipeline returns one pipeline, or domain.ErrNotFound when actor
// cannot see it.
func (e *Executor) GetPipeline(ctx context.Context, actor Actor, id string) (domain.Pipeline, error) {
	pl, err := e.repos.Pipelines.Get(ctx, id)
	if err != nil {
		return domain.Pipeline{}, err
	}
	if !visibleOwner(actor, pl.OwnerID) {
		return domain.Pipeline{}, fmt.Errorf("pipeline %s: %w", id, domain.ErrNotFound)
	}
	return *pl, nil
}

// UpdatePipeline replaces a pipeline's mutable fields, validating the new
// definition exactly as CreatePipeline does.
func (e *Executor) UpdatePipeline(ctx context.Context, actor Actor, id string, in domain.PipelineInput) (domain.Pipeline, error) {
	existing, err := e.mutablePipeline(ctx, actor, id, "update")
	if err != nil {
		return domain.Pipeline{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return domain.Pipeline{}, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	workspaceID := in.WorkspaceID
	if workspaceID == "" {
		workspaceID = existing.WorkspaceID
	}
	res, err := e.Validate(ctx, actor, workspaceID, []byte(in.YAML))
	if err != nil {
		return domain.Pipeline{}, err
	}
	if !res.OK {
		return domain.Pipeline{}, problemsError(res.Problems)
	}

	updated := existing
	if actor.IsAdmin && in.Shared {
		updated.OwnerID = nil
	}
	updated.Name = strings.TrimSpace(in.Name)
	updated.WorkspaceID = workspaceID
	updated.YAML = in.YAML
	updated.UpdatedAt = e.now()
	if err := e.repos.Pipelines.Update(ctx, updated); err != nil {
		return domain.Pipeline{}, wrapConstraint(err)
	}
	return updated, nil
}

// DeletePipeline removes a pipeline and, by cascade, its runs.
func (e *Executor) DeletePipeline(ctx context.Context, actor Actor, id string) error {
	if _, err := e.mutablePipeline(ctx, actor, id, "delete"); err != nil {
		return err
	}
	return e.repos.Pipelines.Delete(ctx, id)
}

// mutablePipeline loads a pipeline actor may change, or returns
// domain.ErrNotFound (invisible) / domain.ErrForbidden (visible but not
// theirs).
func (e *Executor) mutablePipeline(ctx context.Context, actor Actor, id, verb string) (domain.Pipeline, error) {
	pl, err := e.GetPipeline(ctx, actor, id)
	if err != nil {
		return domain.Pipeline{}, err
	}
	if !mutableBy(actor, pl.OwnerID) {
		return domain.Pipeline{}, fmt.Errorf("%w: only the owner or an admin may %s this pipeline", domain.ErrForbidden, verb)
	}
	return pl, nil
}

// Validate parses yamlText and runs the full rule set against
// workspaceID's templates, returning every problem plus the graph — the
// same answer POST /pipelines/validate serves. It never returns an error
// for an invalid definition (that is what Problems is for); the error is
// reserved for a workspace that cannot be read.
func (e *Executor) Validate(ctx context.Context, actor Actor, workspaceID string, yamlText []byte) (ValidationResult, error) {
	def, problems := Parse(yamlText)
	lookup, err := e.lookupFor(ctx, workspaceID)
	if err != nil {
		return ValidationResult{}, err
	}
	if e.repos.Workspaces != nil {
		ws, err := e.repos.Workspaces.Get(ctx, workspaceID)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			problems = append(problems, Problem{Message: fmt.Sprintf("workspace %q not found", workspaceID)})
		case err != nil:
			return ValidationResult{}, err
		case !visibleOwner(actor, ws.OwnerID):
			problems = append(problems, Problem{Message: fmt.Sprintf("workspace %q not found", workspaceID)})
		case def.Workspace != "" && def.Workspace != ws.Name:
			problems = append(problems, Problem{Message: fmt.Sprintf(
				"workspace %q does not match the pipeline's workspace %q", def.Workspace, ws.Name)})
		}
	}
	problems = append(problems, def.Validate(lookup)...)
	return ValidationResult{OK: len(problems) == 0, Problems: problems, Graph: def.Graph()}, nil
}

// lookupFor resolves a workspace's TemplateLookup, tolerating an executor
// wired without one (structural validation only).
func (e *Executor) lookupFor(ctx context.Context, workspaceID string) (TemplateLookup, error) {
	if e.templates == nil {
		return nil, nil
	}
	return e.templates.ForWorkspace(ctx, workspaceID)
}

// problemsError renders validation problems as one domain.ErrInvalid, with
// the yaml line number of each problem that has one.
func problemsError(problems []Problem) error {
	parts := make([]string, 0, len(problems))
	for _, p := range problems {
		if p.Line > 0 {
			parts = append(parts, fmt.Sprintf("line %d: %s", p.Line, p.Message))
		} else {
			parts = append(parts, p.Message)
		}
	}
	return fmt.Errorf("%w: %s", domain.ErrInvalid, strings.Join(parts, "; "))
}

// wrapConstraint turns SQLite's foreign-key complaint into
// domain.ErrInvalid, so a create naming a workspace that does not exist
// reads as a 422 rather than a 500.
func wrapConstraint(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "FOREIGN KEY constraint") {
		return fmt.Errorf("%w: unknown workspace", domain.ErrInvalid)
	}
	return err
}

// --- running ---

// Start creates a pipeline run for pipelineID: one pending step run per
// non-fan-out node (a `foreach` node's step runs appear once its
// dependencies finish and the list is known), then advances the graph,
// which starts every step whose dependencies are already satisfied.
func (e *Executor) Start(ctx context.Context, actor Actor, pipelineID string, input templates.Vars,
	origin domain.Origin, originRef string,
) (domain.PipelineRun, error) {
	pl, err := e.GetPipeline(ctx, actor, pipelineID)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	res, err := e.Validate(ctx, actor, pl.WorkspaceID, []byte(pl.YAML))
	if err != nil {
		return domain.PipelineRun{}, err
	}
	if !res.OK {
		return domain.PipelineRun{}, problemsError(res.Problems)
	}
	def, _ := Parse([]byte(pl.YAML))

	payload, err := json.Marshal(inputOrEmpty(input))
	if err != nil {
		return domain.PipelineRun{}, fmt.Errorf("%w: pipeline input: %v", domain.ErrInvalid, err)
	}
	if origin == "" {
		origin = domain.OriginUI
	}

	pr := domain.PipelineRun{
		ID:         uuid.NewString(),
		PipelineID: pl.ID,
		Origin:     string(origin),
		OriginRef:  originRef,
		Input:      payload,
		State:      domain.PipelineRunRunning,
		StartedAt:  e.now(),
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.repos.PipelineRuns.Create(ctx, pr); err != nil {
		return domain.PipelineRun{}, err
	}
	for _, step := range def.Steps {
		if step.Foreach != "" {
			continue
		}
		if err := e.createStepRun(ctx, pr.ID, step.ID, 0, "", 1); err != nil {
			return domain.PipelineRun{}, err
		}
	}
	e.publishState(pr.ID, "", string(pr.State))
	e.logger.Info("pipelines: started", "pipeline_run_id", pr.ID, "pipeline_id", pl.ID, "origin", origin)
	e.advanceLocked(ctx, pr.ID)
	return pr, nil
}

// inputOrEmpty normalises a nil Vars map to an empty object, so the
// pipeline_runs.input column always holds a JSON object.
func inputOrEmpty(input templates.Vars) templates.Vars {
	if input == nil {
		return templates.Vars{}
	}
	return input
}

// createStepRun inserts one pending step run.
func (e *Executor) createStepRun(ctx context.Context, pipelineRunID, stepID string, index int, item string, attempt int) error {
	return e.repos.StepRuns.Create(ctx, domain.StepRun{
		ID:            uuid.NewString(),
		PipelineRunID: pipelineRunID,
		StepID:        stepID,
		IndexInFanout: index,
		Item:          item,
		Attempt:       attempt,
		State:         domain.StepRunPending,
	})
}

// ListRuns returns the pipeline runs matching filter that actor may see
// (pipeline-run visibility follows its pipeline's), newest first.
func (e *Executor) ListRuns(ctx context.Context, actor Actor, filter domain.PipelineRunFilter) ([]domain.PipelineRun, error) {
	rows, err := e.repos.PipelineRuns.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	visible := map[string]bool{}
	out := make([]domain.PipelineRun, 0, len(rows))
	for _, r := range rows {
		ok, seen := visible[r.PipelineID]
		if !seen {
			_, err := e.GetPipeline(ctx, actor, r.PipelineID)
			ok = err == nil
			visible[r.PipelineID] = ok
		}
		if ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// Cancel stops a running pipeline run by an operator's hand: every running
// step's session is interrupted and closed, every pending step is recorded
// cancelled, and the run itself ends cancelled.
func (e *Executor) Cancel(ctx context.Context, actor Actor, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	pr, err := e.runFor(ctx, actor, id, "cancel")
	if err != nil {
		return err
	}
	if pr.State.Terminal() {
		return fmt.Errorf("%w: pipeline run %s is %s, not running", domain.ErrConflict, id, pr.State)
	}
	rows, err := e.repos.StepRuns.ListByPipelineRun(ctx, id)
	if err != nil {
		return err
	}
	e.stopSteps(ctx, currentAttempts(rows), domain.StepRunCancelled, domain.StepRunCancelled)
	e.finishRun(ctx, pr, domain.PipelineRunCancelled)
	e.logger.Info("pipelines: cancelled", "pipeline_run_id", id, "actor", actor.UserID)
	return nil
}

// RetryFailed re-runs the failed steps of a finished pipeline run: each
// failed step gets a fresh attempt and each step that was skipped behind it
// goes back to pending, then the run is reopened and advanced. Steps that
// already succeeded are left alone, so their reports still feed the retry.
func (e *Executor) RetryFailed(ctx context.Context, actor Actor, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	pr, err := e.runFor(ctx, actor, id, "retry")
	if err != nil {
		return err
	}
	if !pr.State.Terminal() {
		return fmt.Errorf("%w: pipeline run %s is still running", domain.ErrConflict, id)
	}
	rows, err := e.repos.StepRuns.ListByPipelineRun(ctx, id)
	if err != nil {
		return err
	}

	retried := 0
	for _, sr := range currentAttempts(rows) {
		switch sr.State {
		case domain.StepRunFailed:
			if err := e.createStepRun(ctx, id, sr.StepID, sr.IndexInFanout, sr.Item, sr.Attempt+1); err != nil {
				return err
			}
			retried++
		case domain.StepRunSkipped, domain.StepRunCancelled:
			// Never ran, so it costs no attempt: put it back to pending
			// rather than opening a new attempt.
			if err := e.repos.StepRuns.Update(ctx, sr.ID, domain.StepRunPending, nil, nil, sr.Worktree, nil, nil); err != nil {
				return err
			}
			retried++
		}
	}
	if retried == 0 {
		return fmt.Errorf("%w: pipeline run %s has no failed steps to retry", domain.ErrConflict, id)
	}

	// Reopen, not Finish: the run is going again, so finished_at goes back
	// to NULL rather than being re-stamped with "now" on a run that has not
	// finished (which the run page would then read as its elapsed time).
	if err := e.repos.PipelineRuns.Reopen(ctx, id); err != nil {
		return err
	}
	e.publishState(id, "", string(domain.PipelineRunRunning))
	e.logger.Info("pipelines: retrying failed steps", "pipeline_run_id", id, "steps", retried, "actor", actor.UserID)
	e.advanceLocked(ctx, id)
	return nil
}

// runFor loads a pipeline run whose pipeline actor may act on.
func (e *Executor) runFor(ctx context.Context, actor Actor, id, verb string) (domain.PipelineRun, error) {
	pr, err := e.repos.PipelineRuns.Get(ctx, id)
	if err != nil {
		return domain.PipelineRun{}, err
	}
	if _, err := e.mutablePipeline(ctx, actor, pr.PipelineID, verb+" a run of"); err != nil {
		return domain.PipelineRun{}, err
	}
	return *pr, nil
}

// --- bus loop and sweep ---

// Run follows every live pipeline run until ctx is cancelled: it advances
// graphs as the runs behind their steps finish on the event bus, and sweeps
// for pipeline runs that overran their timeout every 30s. It is meant to be
// started in its own goroutine at serve time.
func (e *Executor) Run(ctx context.Context) {
	ch, unsubscribe := e.bus.Subscribe(ctx, busBuffer)
	defer unsubscribe()

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.Tick(ctx, e.now())
		case msg, ok := <-ch:
			if !ok {
				return
			}
			e.handle(ctx, msg)
		}
	}
}

// handle applies one bus message: a run that just finished advances the
// pipeline run of the step it belonged to, if any.
func (e *Executor) handle(ctx context.Context, msg events.Message) {
	if msg.Kind != runs.EventFinished && msg.Kind != runs.EventFailed {
		return
	}
	var payload struct {
		RunID     string  `json:"run_id"`
		StepRunID *string `json:"step_run_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}
	if payload.StepRunID == nil || payload.RunID == "" {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.completeStep(ctx, payload.RunID)
}

// Tick closes out every pipeline run that overran its definition's timeout
// — running step sessions are interrupted and closed, pending steps are
// cancelled and the run is recorded as timed out — and reconciles the runs
// whose finish message never reached this process (a dropped bus message).
// Run calls it every 30s; tests call it directly with a controlled now.
func (e *Executor) Tick(ctx context.Context, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	running, err := e.repos.PipelineRuns.ListRunningOlderThan(ctx, now)
	if err != nil {
		e.logger.Error("pipelines: list running pipeline runs", "error", err)
		return
	}
	for _, pr := range running {
		_, def, ok := e.definitionOf(ctx, pr)
		timeout := DefaultTimeout
		if ok && def.Timeout > 0 {
			timeout = def.Timeout
		}
		if now.Sub(pr.StartedAt) < timeout {
			e.reconcile(ctx, pr)
			continue
		}
		rows, err := e.repos.StepRuns.ListByPipelineRun(ctx, pr.ID)
		if err != nil {
			e.logger.Error("pipelines: list step runs", "pipeline_run_id", pr.ID, "error", err)
			continue
		}
		e.stopSteps(ctx, currentAttempts(rows), domain.StepRunCancelled, domain.StepRunCancelled)
		e.finishRun(ctx, pr, domain.PipelineRunTimeout)
		e.logger.Warn("pipelines: timed out", "pipeline_run_id", pr.ID, "timeout", timeout)
	}
}

// reconcile picks up step runs whose run has already finished but whose bus
// message this process never saw (it was dropped, or the server restarted).
func (e *Executor) reconcile(ctx context.Context, pr domain.PipelineRun) {
	rows, err := e.repos.StepRuns.ListByPipelineRun(ctx, pr.ID)
	if err != nil {
		return
	}
	for _, sr := range rows {
		if sr.State != domain.StepRunRunning || sr.RunID == nil {
			continue
		}
		run, err := e.repos.Runs.Get(ctx, *sr.RunID)
		if err != nil || run.Outcome == domain.RunRunning {
			continue
		}
		e.completeStep(ctx, run.ID)
	}
}
