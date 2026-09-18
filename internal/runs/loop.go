package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/notify"
)

// busBuffer is the engine's bus subscription depth. Runs are low-volume but
// a session's transcript is not, and a dropped message would strand a run
// as running until the timeout sweep, so the buffer is generous.
const busBuffer = 512

// sweepInterval is how often Run closes out timed-out runs.
const sweepInterval = time.Minute

// reconcileEvents bounds the transcript read Start does to catch a session
// that finished before its run row existed. A result is the last event of a
// turn and an unattended run's first turn is short, so the window is small.
const reconcileEvents = 500

// onceSet remembers ids something has already been done for, e.g. the
// run.needs_human notification that must only be sent once per run.
type onceSet struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func newOnceSet() *onceSet { return &onceSet{ids: make(map[string]struct{})} }

// do reports whether id is new to the set (and records it), so callers can
// write `if s.do(id) { ... }` for at-most-once behaviour.
func (s *onceSet) do(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.ids[id]; seen {
		return false
	}
	s.ids[id] = struct{}{}
	return true
}

// forget drops id, so the set only holds ids still in play.
func (s *onceSet) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ids, id)
}

// Run follows every live run until ctx is cancelled: it subscribes to the
// event bus and reacts to the sessions its runs drive, and sweeps
// timed-out runs once a minute. It is meant to be started in its own
// goroutine at serve time.
func (e *Engine) Run(ctx context.Context) {
	ch, unsubscribe := e.bus.Subscribe(ctx, busBuffer)
	defer unsubscribe()

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.Tick(ctx, time.Now())
		case msg, ok := <-ch:
			if !ok {
				return
			}
			e.handle(ctx, msg)
		}
	}
}

// handle applies one bus message to the run (if any) that owns the message's
// session.
func (e *Engine) handle(ctx context.Context, msg events.Message) {
	switch msg.Kind {
	case "session.event":
		e.handleSessionEvent(ctx, msg)
	case "session.state":
		e.handleSessionState(ctx, msg)
	case "approval.created":
		e.handleApprovalCreated(ctx, msg)
	}
}

// runFor returns the still-running run driving sessionID, or false when
// there is none (an interactive session, or a run already closed out).
func (e *Engine) runFor(ctx context.Context, sessionID string) (domain.Run, bool) {
	if sessionID == "" {
		return domain.Run{}, false
	}
	run, err := e.repos.Runs.GetBySession(ctx, sessionID)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			e.logger.Error("runs: look up run by session", "session_id", sessionID, "error", err)
		}
		return domain.Run{}, false
	}
	if run.Outcome != domain.RunRunning {
		return domain.Run{}, false
	}
	return *run, true
}

// handleSessionEvent closes a run out successfully when its session reports
// a result: the structured report the schema-constrained turn produced, or
// the model's final text when there is none.
func (e *Engine) handleSessionEvent(ctx context.Context, msg events.Message) {
	var ev harness.Event
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		return
	}
	if ev.Type != harness.EventResult || ev.Result == nil {
		return
	}
	run, ok := e.runFor(ctx, msg.SessionID)
	if !ok {
		return
	}
	e.completeFromResult(ctx, run, *ev.Result)
}

// completeFromResult closes a run out from the result its session reported:
// success with the structured report, or failed when the CLI flagged the
// turn as an error.
func (e *Engine) completeFromResult(ctx context.Context, run domain.Run, res harness.Result) {
	report := reportOf(res)
	summary := summaryOf(report, res.Text)
	cost := e.costOf(ctx, run.SessionID, res.CostUSD)

	outcome := domain.RunSuccess
	kind := EventFinished
	if res.IsError {
		outcome = domain.RunFailed
		kind = EventFailed
		if summary == "" {
			summary = "the session reported an error"
		}
	}
	if !e.finish(ctx, run, outcome, report, summary, cost) {
		return
	}
	e.notify(ctx, kind, run.ID, string(outcome), summary)
}

// reconcile closes out a run whose session already ended before the run row
// existed. sessions.Create starts the process and sends its first turn
// before Start can insert the run, so a fast session's result can reach the
// bus while no run row resolves it yet. Every event is persisted before it
// is published, so the stored transcript is the authority; the completed
// once-set makes sure only one of the two paths actually closes the run.
func (e *Engine) reconcile(ctx context.Context, run domain.Run) {
	if e.repos.Events == nil {
		return
	}
	rows, err := e.repos.Events.ListAfter(ctx, run.SessionID, 0, reconcileEvents)
	if err != nil {
		e.logger.Error("runs: reconcile transcript", "run_id", run.ID, "error", err)
		return
	}
	for _, row := range rows {
		if row.Type != string(harness.EventResult) {
			continue
		}
		var ev harness.Event
		if err := json.Unmarshal(row.Payload, &ev); err != nil || ev.Result == nil {
			continue
		}
		e.completeFromResult(ctx, run, *ev.Result)
		return
	}
	if sess, err := e.repos.Sessions.Get(ctx, run.SessionID); err == nil && sess.State == domain.SessionFailed {
		e.failRun(ctx, run)
	}
}

// handleSessionState fails a run whose session failed (its process died, or
// styr could not record an approval).
func (e *Engine) handleSessionState(ctx context.Context, msg events.Message) {
	var payload struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}
	if domain.SessionState(payload.State) != domain.SessionFailed {
		return
	}
	run, ok := e.runFor(ctx, msg.SessionID)
	if !ok {
		return
	}
	e.failRun(ctx, run)
}

// failRun records a run whose session failed and pages the operator.
func (e *Engine) failRun(ctx context.Context, run domain.Run) {
	const summary = "the session failed"
	if !e.finish(ctx, run, domain.RunFailed, nil, summary, e.costOf(ctx, run.SessionID, 0)) {
		return
	}
	e.notify(ctx, EventFailed, run.ID, string(domain.RunFailed), summary)
}

// handleApprovalCreated pages the operator the first time a run's
// unattended session asks for a human decision. The run stays running: the
// approval may still be answered (or expire and deny) without ending it.
func (e *Engine) handleApprovalCreated(ctx context.Context, msg events.Message) {
	run, ok := e.runFor(ctx, msg.SessionID)
	if !ok {
		return
	}
	if !e.notified.do(run.ID) {
		return
	}
	e.notify(ctx, EventNeedsHuman, run.ID, "needs human", "an unattended run is waiting for an approval")
}

// Tick closes out every run still running past the engine's timeout: the
// session is interrupted and closed, the run is recorded as timed out and
// the operator is notified. Run calls it once a minute; tests call it
// directly with a controlled now.
func (e *Engine) Tick(ctx context.Context, now time.Time) {
	stale, err := e.repos.Runs.ListRunningOlderThan(ctx, now.Add(-e.timeout))
	if err != nil {
		e.logger.Error("runs: list timed-out runs", "error", err)
		return
	}
	for _, run := range stale {
		summary := fmt.Sprintf("timed out after %s", e.timeout)
		if !e.finish(ctx, run, domain.RunTimeout, nil, summary, e.costOf(ctx, run.SessionID, 0)) {
			continue
		}
		if err := e.sessions.Interrupt(ctx, serviceActor, run.SessionID); err != nil {
			e.logger.Info("runs: interrupt timed-out session", "run_id", run.ID, "session_id", run.SessionID, "error", err)
		}
		if err := e.sessions.Close(ctx, serviceActor, run.SessionID); err != nil {
			e.logger.Error("runs: close timed-out session", "run_id", run.ID, "session_id", run.SessionID, "error", err)
		}
		e.notify(ctx, EventFailed, run.ID, string(domain.RunTimeout), summary)
	}
}

// finish persists a run's outcome, report, summary and cost, at most once
// per run for the life of this process. It reports whether this call was
// the one that closed the run, so only that caller notifies.
func (e *Engine) finish(ctx context.Context, run domain.Run, outcome domain.RunOutcome, report json.RawMessage, summary string, cost float64) bool {
	if !e.completed.do(run.ID) {
		return false
	}
	if report == nil {
		report = json.RawMessage("{}")
	}
	if err := e.repos.Runs.Finish(ctx, run.ID, outcome, report, summary, cost); err != nil {
		e.logger.Error("runs: finish", "run_id", run.ID, "outcome", outcome, "error", err)
		return false
	}
	e.notified.forget(run.ID)
	e.logger.Info("runs: finished", "run_id", run.ID, "outcome", outcome)
	// Every path that closes a run out goes through finish, so this is the
	// one place a loop has to learn that one of its iterations ended.
	e.advance(ctx, run, outcome, report)
	return true
}

// costOf returns the session's accumulated cost, falling back to (and never
// under-reporting) the cost of the result event that closed the run.
func (e *Engine) costOf(ctx context.Context, sessionID string, eventCost float64) float64 {
	if sess, err := e.repos.Sessions.Get(ctx, sessionID); err == nil && sess.CostUSD > eventCost {
		return sess.CostUSD
	}
	return eventCost
}

// reportOf returns the run's report: the structured output the CLI produced
// under the template's schema, or the model's final text wrapped as
// {"result": ...} when there is no structured output.
func reportOf(res harness.Result) json.RawMessage {
	if len(res.StructuredOutput) > 0 && !isJSONNull(res.StructuredOutput) {
		return res.StructuredOutput
	}
	wrapped, err := json.Marshal(map[string]string{"result": res.Text})
	if err != nil {
		return json.RawMessage("{}")
	}
	return wrapped
}

// isJSONNull reports whether raw is the JSON literal null, which the codec
// produces for a result envelope with an explicit "structured_output": null.
func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// summaryOf returns the run's one-line summary: the report's diagnosis
// field when the schema produced one, otherwise the model's final text,
// truncated either way.
func summaryOf(report json.RawMessage, text string) string {
	var fields struct {
		Diagnosis string `json:"diagnosis"`
	}
	if err := json.Unmarshal(report, &fields); err == nil && strings.TrimSpace(fields.Diagnosis) != "" {
		return truncate(strings.TrimSpace(fields.Diagnosis), summaryLimit)
	}
	return truncate(strings.TrimSpace(text), summaryLimit)
}

// notify publishes one run event to every enabled channel subscribed to
// kind. A notification failure never affects the run: it is logged and
// dropped.
func (e *Engine) notify(ctx context.Context, kind, runID, outcome, body string) {
	if e.notifier == nil {
		return
	}
	rows, err := e.repos.Channels.ListEnabledFor(ctx, kind)
	if err != nil {
		e.logger.Error("runs: list notification channels", "event_kind", kind, "error", err)
		return
	}
	channels := make([]notify.Channel, 0, len(rows))
	for _, row := range rows {
		ch := notify.Channel{ID: row.ID, Kind: string(row.Kind), Name: row.Name, URL: row.URL, Events: row.Events}
		if len(row.TokenCiphertext) > 0 && len(row.TokenNonce) > 0 {
			plain, err := e.box.Open(row.TokenCiphertext, row.TokenNonce)
			if err != nil {
				// Never log the ciphertext or the failure's contents beyond
				// the channel id: an unreadable token is a configuration
				// problem, not a secret to print.
				e.logger.Error("runs: decrypt channel token", "channel_id", row.ID, "error", err)
				continue
			}
			ch.Token = string(plain)
		}
		channels = append(channels, ch)
	}

	e.notifier.Publish(ctx, channels, notify.Event{
		Kind:     kind,
		Title:    fmt.Sprintf("%s: %s", outcome, e.runTitle(ctx, runID)),
		Body:     body,
		URL:      e.baseURL + "/runs/" + runID,
		Priority: priorityFor(kind),
		Tags:     tagsFor(kind),
	})
}

// runTitle returns the title of the session a run drives, which is the
// human-readable name of the run itself (a run row carries no title of its
// own).
func (e *Engine) runTitle(ctx context.Context, runID string) string {
	run, err := e.repos.Runs.Get(ctx, runID)
	if err != nil {
		return runID
	}
	sess, err := e.repos.Sessions.Get(ctx, run.SessionID)
	if err != nil || sess.Title == "" {
		return runID
	}
	return sess.Title
}

// priorityFor maps an event kind to an ntfy priority: anything that needs
// an operator's attention now is raised above the default.
func priorityFor(kind string) int {
	switch kind {
	case EventNeedsHuman, EventFailed:
		return 4
	default:
		return 3
	}
}

func tagsFor(kind string) []string {
	switch kind {
	case EventNeedsHuman:
		return []string{"raised_hand", "styr"}
	case EventFailed:
		return []string{"rotating_light", "styr"}
	default:
		return []string{"white_check_mark", "styr"}
	}
}
