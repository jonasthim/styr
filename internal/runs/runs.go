// Package runs is the unattended run engine: it turns a rendered template
// into a session, tracks that session's life on the event bus, and closes
// the run out with a structured report plus an outbound notification. A run
// row is the operator-facing record of an unattended session — what started
// it (template, trigger, delivery), how it ended and what it found.
package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/notify"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/templates"
)

// Event kinds published to notification channels, matching the `events`
// column of notification_channels.
const (
	EventFinished   = "run.finished"
	EventNeedsHuman = "run.needs_human"
	EventFailed     = "run.failed"
)

// summaryLimit caps a run's one-line summary (report diagnosis, or the
// model's final text when there is no report).
const summaryLimit = 200

// titleLimit caps a rendered run title, so a template that interpolates a
// whole payload cannot write an unbounded session title.
const titleLimit = 200

// DefaultTimeout is how long a run may stay running before the timeout
// sweep closes it, when New is given a non-positive timeout.
const DefaultTimeout = 30 * time.Minute

// serviceActor is the actor the engine uses against sessions.Service: an
// unattended run has no human owner, so it acts with admin visibility and
// creates sessions with a nil owner (the service token).
var serviceActor = sessions.Actor{UserID: "", IsAdmin: true}

// Repos bundles the repositories the engine reads and writes.
type Repos struct {
	Runs       *db.Runs
	Templates  *db.Templates
	Sessions   *db.Sessions
	Channels   *db.NotificationChannels
	Deliveries *db.Deliveries
	// Events is the session transcript, read once per run to close the
	// window between a session starting and its run row existing (see
	// Engine.reconcile).
	Events *db.Events
}

// Notifier publishes a run event to a set of already-resolved channels.
// *notify.Service implements it; tests substitute a recorder.
type Notifier interface {
	Publish(ctx context.Context, channels []notify.Channel, ev notify.Event)
}

// RunInput describes an unattended run to start: which template to render,
// with which variables, and which trigger delivery (if any) caused it.
type RunInput struct {
	TemplateID string
	TriggerID  string
	DeliveryID string
	Vars       templates.Vars
	Origin     domain.Origin
}

// Engine starts unattended runs and follows them to completion.
type Engine struct {
	repos    Repos
	sessions *sessions.Service
	bus      *events.Bus
	notifier Notifier
	box      *crypto.Box
	baseURL  string
	timeout  time.Duration
	logger   *slog.Logger

	// notified records the run ids a run.needs_human notification has
	// already been sent for, so a session that asks for several approvals
	// only pages the operator once per run. completed does the same for
	// the single write that closes a run out, so the bus handler and the
	// post-start reconciliation can never both finish (and notify) the
	// same run. Both hold only run ids started or observed by this
	// process.
	notified  *onceSet
	completed *onceSet
}

// New constructs an Engine. baseURL is the public URL used to build the
// deep link in notifications; timeout is how long a run may stay running
// before the sweep closes it as timed out (DefaultTimeout when
// non-positive).
func New(repos Repos, sessionsSvc *sessions.Service, bus *events.Bus, notifier Notifier, box *crypto.Box, baseURL string, timeout time.Duration, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Engine{
		repos:     repos,
		sessions:  sessionsSvc,
		bus:       bus,
		notifier:  notifier,
		box:       box,
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		timeout:   timeout,
		logger:    logger,
		notified:  newOnceSet(),
		completed: newOnceSet(),
	}
}

// Start renders in's template, creates the unattended session it describes
// and records a running run for it. The run id is generated first and
// handed to the session as its OriginRef, so the session points back at the
// run that owns it.
func (e *Engine) Start(ctx context.Context, in RunInput) (domain.Run, error) {
	tpl, err := e.repos.Templates.Get(ctx, in.TemplateID)
	if err != nil {
		return domain.Run{}, err
	}

	title := e.renderTitle(*tpl, in.Vars)
	prompt, err := templates.Render(tpl.PromptTemplate, in.Vars)
	if err != nil {
		return domain.Run{}, fmt.Errorf("runs: render prompt: %w", err)
	}
	if strings.TrimSpace(prompt) == "" {
		return domain.Run{}, fmt.Errorf("%w: template %s rendered an empty prompt", domain.ErrInvalid, tpl.ID)
	}

	origin := in.Origin
	if origin == "" {
		origin = domain.OriginWebhook
	}

	runID := uuid.NewString()
	sess, err := e.sessions.Create(ctx, serviceActor, sessions.CreateInput{
		WorkspaceID:  tpl.WorkspaceID,
		ProfileID:    tpl.ProfileID,
		Title:        title,
		Prompt:       prompt,
		Origin:       origin,
		Owner:        nil,
		RunID:        runID,
		JSONSchema:   tpl.ReportSchema,
		SystemPrompt: tpl.SystemPrompt,
	})
	if err != nil {
		return domain.Run{}, err
	}

	run := domain.Run{
		ID:         runID,
		SessionID:  sess.ID,
		TemplateID: &tpl.ID,
		TriggerID:  optional(in.TriggerID),
		DeliveryID: optional(in.DeliveryID),
		Origin:     string(origin),
		StartedAt:  time.Now(),
		Outcome:    domain.RunRunning,
		Report:     json.RawMessage("{}"),
	}
	if err := e.repos.Runs.Create(ctx, run); err != nil {
		// The session is already live but nothing will ever follow it:
		// close it rather than leaving an orphan process running.
		if cErr := e.sessions.Close(ctx, serviceActor, sess.ID); cErr != nil {
			e.logger.Error("runs: close orphaned session", "session_id", sess.ID, "error", cErr)
		}
		return domain.Run{}, err
	}
	e.logger.Info("runs: started", "run_id", run.ID, "session_id", sess.ID, "template_id", tpl.ID, "origin", origin)
	e.reconcile(ctx, run)
	return run, nil
}

// renderTitle renders the template's title, falling back to the template's
// name when it has no title template or rendering fails (a run must always
// have a readable title, and a bad title is not worth failing a run over).
func (e *Engine) renderTitle(tpl domain.Template, vars templates.Vars) string {
	title := ""
	if tpl.TitleTemplate != "" {
		rendered, err := templates.Render(tpl.TitleTemplate, vars)
		if err != nil {
			e.logger.Warn("runs: render title", "template_id", tpl.ID, "error", err)
		} else {
			title = strings.TrimSpace(rendered)
		}
	}
	if title == "" {
		title = tpl.Name
	}
	return truncate(title, titleLimit)
}

// optional turns an empty id into a nil pointer, for the run's nullable
// template/trigger/delivery columns.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// truncate shortens s to at most n runes.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
