package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/templates"
)

// defaultLoopMax is the iteration budget a looping template gets when it
// names none, matching the loops table's max_iterations default.
const defaultLoopMax = 5

// reportPreviewLimit caps the previous report embedded in the next
// iteration's prompt, so one enormous report cannot fill the context on its
// own. The full report stays on the previous run row.
const reportPreviewLimit = 4000

// LoopView is a loop together with what a UI needs to show it: every
// iteration in order, and the template all of them render.
type LoopView struct {
	Loop     domain.Loop
	Runs     []domain.Run
	Template *domain.Template
}

// varsCache remembers the variables each live loop's template was first
// rendered with. They are deliberately not persisted: the loops table (see
// migration 00007) holds no payload, and a loop only outlives the process
// that started it as history. A loop whose vars are gone still iterates —
// its prompt simply renders the missing keys empty, exactly as a first run
// against an incomplete payload would.
type varsCache struct {
	mu   sync.Mutex
	vars map[string]templates.Vars
}

func newVarsCache() *varsCache { return &varsCache{vars: make(map[string]templates.Vars)} }

func (c *varsCache) set(id string, v templates.Vars) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vars[id] = v
}

// get returns a copy of the variables recorded for id, so a caller may add
// the per-iteration `loop` variable without mutating the stored set.
func (c *varsCache) get(id string) templates.Vars {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(templates.Vars, len(c.vars[id])+1)
	for k, v := range c.vars[id] {
		out[k] = v
	}
	return out
}

func (c *varsCache) forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.vars, id)
}

// StartManual starts a run of templateID by hand, as the UI's "run this
// template now" does: origin ui, no owner (the session is unattended like
// every other run). A template carrying loop fields loops from here just as
// it would from a webhook.
func (e *Engine) StartManual(ctx context.Context, actor sessions.Actor, templateID string, vars templates.Vars) (domain.Run, error) {
	run, err := e.Start(ctx, RunInput{TemplateID: templateID, Vars: vars, Origin: domain.OriginUI})
	if err != nil {
		return domain.Run{}, err
	}
	e.logger.Info("runs: started manually", "run_id", run.ID, "template_id", templateID, "actor", actor.UserID)
	return run, nil
}

// startLoop records the loop a looping template's first run belongs to. It
// returns the loop's id and the run's iteration number, or empty/0 when the
// template does not loop.
func (e *Engine) startLoop(ctx context.Context, tpl domain.Template, in RunInput, sessionID string, origin domain.Origin) (string, int, error) {
	if tpl.LoopUntil == "" {
		return "", 0, nil
	}
	if e.repos.Loops == nil {
		// Nothing can drive the iterations, so run the template once
		// rather than pretending to loop.
		e.logger.Warn("runs: template loops but no loops repository is configured", "template_id", tpl.ID)
		return "", 0, nil
	}
	max := tpl.LoopMax
	if max < 1 {
		max = defaultLoopMax
	}
	now := time.Now()
	loop := domain.Loop{
		ID:            uuid.NewString(),
		TemplateID:    tpl.ID,
		SessionID:     &sessionID,
		Origin:        string(origin),
		OriginRef:     in.TriggerID,
		UntilField:    tpl.LoopUntil,
		MaxIterations: max,
		Iteration:     1,
		State:         domain.LoopRunning,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := e.repos.Loops.Create(ctx, loop); err != nil {
		return "", 0, err
	}
	e.loopVars.set(loop.ID, in.Vars)
	e.logger.Info("runs: loop started", "loop_id", loop.ID, "template_id", tpl.ID,
		"until", loop.UntilField, "max", loop.MaxIterations)
	return loop.ID, 1, nil
}

// advance moves the loop a just-finished run belongs to: it ends the loop
// when the report says done, when the iteration budget is spent or when the
// run did not succeed, and otherwise sends the next iteration's prompt on
// the loop's session. It is a no-op for a run outside a loop, and for a
// loop that is no longer running.
func (e *Engine) advance(ctx context.Context, run domain.Run, outcome domain.RunOutcome, report json.RawMessage) {
	if run.LoopID == "" || e.repos.Loops == nil {
		return
	}
	loop, err := e.repos.Loops.Get(ctx, run.LoopID)
	if err != nil {
		e.logger.Error("runs: load loop", "loop_id", run.LoopID, "error", err)
		return
	}
	if loop.State != domain.LoopRunning {
		return
	}
	switch {
	case outcome != domain.RunSuccess:
		e.endLoop(ctx, *loop, run.Iteration, domain.LoopFailed)
	case loopDone(report, loop.UntilField):
		e.endLoop(ctx, *loop, run.Iteration, domain.LoopDone)
	case run.Iteration >= loop.MaxIterations:
		e.endLoop(ctx, *loop, run.Iteration, domain.LoopExhausted)
	default:
		if err := e.nextIteration(ctx, *loop, run, report); err != nil {
			e.logger.Error("runs: start next loop iteration", "loop_id", loop.ID, "error", err)
			e.endLoop(ctx, *loop, run.Iteration, domain.LoopFailed)
		}
	}
}

// endLoop records a loop's final state and drops its cached variables.
func (e *Engine) endLoop(ctx context.Context, loop domain.Loop, iteration int, state domain.LoopState) {
	if iteration < loop.Iteration {
		iteration = loop.Iteration
	}
	if err := e.repos.Loops.Update(ctx, loop.ID, state, iteration, nil); err != nil {
		e.logger.Error("runs: end loop", "loop_id", loop.ID, "state", state, "error", err)
		return
	}
	e.loopVars.forget(loop.ID)
	e.logger.Info("runs: loop ended", "loop_id", loop.ID, "state", state, "iterations", iteration)
}

// nextIteration renders the loop's template again — same variables, plus a
// `loop` variable describing where the loop stands — and sends it on the
// loop's own session, so the model keeps the whole conversation. The run
// row for the new iteration is written before the prompt is sent: the
// result may come back before Send returns, and the engine resolves a
// session's live events through its newest run.
func (e *Engine) nextIteration(ctx context.Context, loop domain.Loop, prev domain.Run, report json.RawMessage) error {
	if loop.SessionID == nil {
		return fmt.Errorf("%w: loop %s has no session", domain.ErrConflict, loop.ID)
	}
	tpl, err := e.repos.Templates.Get(ctx, loop.TemplateID)
	if err != nil {
		return err
	}
	next := prev.Iteration + 1
	previous := previousReport(report)

	vars := e.loopVars.get(loop.ID)
	vars["loop"] = map[string]any{
		"iteration":       next,
		"max":             loop.MaxIterations,
		"previous_report": previous,
	}
	prompt, err := templates.Render(tpl.PromptTemplate, vars)
	if err != nil {
		return fmt.Errorf("runs: render loop prompt: %w", err)
	}
	text := fmt.Sprintf("Iteration %d of %d. Previous report: %s\n\n%s",
		next, loop.MaxIterations, truncate(compactJSON(report), reportPreviewLimit), strings.TrimSpace(prompt))

	run := domain.Run{
		ID:         uuid.NewString(),
		SessionID:  *loop.SessionID,
		TemplateID: &tpl.ID,
		TriggerID:  prev.TriggerID,
		DeliveryID: prev.DeliveryID,
		Origin:     loop.Origin,
		LoopID:     loop.ID,
		Iteration:  next,
		StartedAt:  time.Now(),
		Outcome:    domain.RunRunning,
		Report:     json.RawMessage("{}"),
	}
	if err := e.repos.Runs.Create(ctx, run); err != nil {
		return err
	}
	if err := e.repos.Loops.Update(ctx, loop.ID, domain.LoopRunning, next, nil); err != nil {
		return err
	}
	if err := e.sessions.Send(ctx, serviceActor, *loop.SessionID, text); err != nil {
		// The row exists but nothing will ever drive it; close it out so
		// the iteration is not left running until the timeout sweep.
		e.finish(ctx, run, domain.RunFailed, nil, "the next loop iteration could not be sent", 0)
		return err
	}
	e.logger.Info("runs: loop iteration", "loop_id", loop.ID, "run_id", run.ID, "iteration", next)
	return nil
}

// previousReport decodes a report for the `loop.previous_report` template
// variable, so a prompt can index into it; an undecodable report is passed
// through as its raw text.
func previousReport(report json.RawMessage) any {
	var decoded any
	if err := json.Unmarshal(report, &decoded); err != nil {
		return string(report)
	}
	return decoded
}

// compactJSON strips insignificant whitespace from a report for the prompt
// line that carries it, falling back to the raw text.
func compactJSON(report json.RawMessage) string {
	if len(report) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, report); err != nil {
		return string(report)
	}
	return buf.String()
}

// loopDone reports whether a report says the loop's work is finished:
// report[field] must be present and truthy. A missing field, false, 0, an
// empty string and the negative words ("no", "false", "0", "off") all mean
// "keep going", per the v0.4 plan.
func loopDone(report json.RawMessage, field string) bool {
	if field == "" {
		field = domain.DefaultLoopUntilField
	}
	var fields map[string]any
	if err := json.Unmarshal(report, &fields); err != nil {
		return false
	}
	value, ok := fields[field]
	if !ok {
		return false
	}
	return truthy(value)
}

// truthy is the loop's notion of "done": everything is true except the
// explicit falsehoods.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case json.Number:
		return v.String() != "0" && v.String() != ""
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "no", "n", "false", "0", "off":
			return false
		}
		return true
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// GetLoop returns one loop with its iterations, in order, and the template
// they all render.
func (e *Engine) GetLoop(ctx context.Context, id string) (LoopView, error) {
	if e.repos.Loops == nil {
		return LoopView{}, fmt.Errorf("loop %s: %w", id, domain.ErrNotFound)
	}
	loop, err := e.repos.Loops.Get(ctx, id)
	if err != nil {
		return LoopView{}, err
	}
	view := LoopView{Loop: *loop}
	rows, err := e.repos.Runs.ListByLoop(ctx, id)
	if err != nil {
		return LoopView{}, err
	}
	view.Runs = rows
	if tpl, err := e.repos.Templates.Get(ctx, loop.TemplateID); err == nil {
		view.Template = tpl
	}
	return view, nil
}

// ListLoops returns loops in state (every state when state is empty),
// newest first.
func (e *Engine) ListLoops(ctx context.Context, state string, limit int) ([]domain.Loop, error) {
	if e.repos.Loops == nil {
		return nil, nil
	}
	return e.repos.Loops.List(ctx, state, limit)
}

// Stop ends a running loop by an operator's hand: the loop is recorded as
// stopped and its session is closed, so the iteration in flight stops
// costing anything. Stopping a loop that is not running is a conflict.
func (e *Engine) Stop(ctx context.Context, actor sessions.Actor, id string) error {
	if e.repos.Loops == nil {
		return fmt.Errorf("loop %s: %w", id, domain.ErrNotFound)
	}
	loop, err := e.repos.Loops.Get(ctx, id)
	if err != nil {
		return err
	}
	if loop.State != domain.LoopRunning {
		return fmt.Errorf("%w: loop %s is %s, not running", domain.ErrConflict, id, loop.State)
	}
	if err := e.repos.Loops.Update(ctx, id, domain.LoopStopped, loop.Iteration, nil); err != nil {
		return err
	}
	e.loopVars.forget(id)
	if loop.SessionID != nil {
		// The session belongs to no user, so it is closed with the
		// engine's service actor rather than the operator's.
		if err := e.sessions.Close(ctx, serviceActor, *loop.SessionID); err != nil {
			e.logger.Error("runs: close stopped loop session", "loop_id", id, "session_id", *loop.SessionID, "error", err)
		}
	}
	e.logger.Info("runs: loop stopped", "loop_id", id, "actor", actor.UserID)
	return nil
}
