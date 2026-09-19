// Package api wires Styr's HTTP surface: a chi router, middleware, JSON
// helpers and one *_handlers.go file per resource. Handlers call services;
// they contain no business logic beyond decoding, role checks and shaping
// responses.
package api

import (
	"context"
	"time"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/notify"
	"github.com/jonasthim/styr/internal/pipelines"
	"github.com/jonasthim/styr/internal/runs"
	"github.com/jonasthim/styr/internal/schedules"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/stats"
	"github.com/jonasthim/styr/internal/templates"
	"github.com/jonasthim/styr/internal/workspaces"
)

// Actor is sessions.Actor, re-exported so triggers/runs handlers (and the
// TriggersService/RunsEngine interfaces below) don't need their own import
// of internal/sessions just for this one type.
type Actor = sessions.Actor

// TriggersService is what internal/api's templates, triggers, deliveries
// and inbound-hook handlers call. Card T34 implements it as
// internal/triggers.Service; here it is declared as an interface so T35
// compiles and tests independently of T34, with the orchestrator wiring the
// concrete type into Deps.Triggers at merge.
type TriggersService interface {
	CreateTemplate(ctx context.Context, actor Actor, in domain.TemplateInput) (domain.Template, error)
	ListTemplates(ctx context.Context, actor Actor) ([]domain.Template, error)
	GetTemplate(ctx context.Context, actor Actor, id string) (domain.Template, error)
	UpdateTemplate(ctx context.Context, actor Actor, id string, in domain.TemplateInput) (domain.Template, error)
	DeleteTemplate(ctx context.Context, actor Actor, id string) error
	RenderTemplate(ctx context.Context, actor Actor, id, kind string, payload []byte) (domain.RenderResult, error)

	CreateTrigger(ctx context.Context, actor Actor, in domain.TriggerInput) (domain.Trigger, string, error)
	ListTriggers(ctx context.Context, actor Actor) ([]domain.Trigger, error)
	GetTrigger(ctx context.Context, actor Actor, id string) (domain.Trigger, error)
	UpdateTrigger(ctx context.Context, actor Actor, id string, in domain.TriggerInput) (domain.Trigger, error)
	DeleteTrigger(ctx context.Context, actor Actor, id string) error
	RotateSecret(ctx context.Context, actor Actor, id string) (string, error)

	ListDeliveries(ctx context.Context, actor Actor, triggerID string, limit int) ([]domain.Delivery, error)
	Replay(ctx context.Context, actor Actor, deliveryID string) (domain.Delivery, error)
	Test(ctx context.Context, actor Actor, triggerID string, payload []byte, force bool) (domain.Delivery, error)

	// Deliver handles one inbound POST /hooks/{slug} call: secret/HMAC
	// check, dedupe, cooldown, storm cap, delivery log and (when accepted)
	// starting a run. It is unauthenticated at the HTTP layer — in.Slug
	// and in.Headers carry everything Deliver needs to authenticate the
	// caller itself, returning domain.ErrUnknownTrigger or
	// domain.ErrBadSecret for the two documented failure modes.
	Deliver(ctx context.Context, in domain.Inbound) (domain.Delivery, error)
}

// RunsEngine is what internal/api's runs, templates and loops handlers
// call. Card T34 implements it as internal/runs.Engine; see
// TriggersService's doc comment for why this is an interface here.
type RunsEngine interface {
	Get(ctx context.Context, id string) (domain.RunView, error)
	List(ctx context.Context, f domain.RunFilter) ([]domain.RunView, error)

	// StartManual starts a run of templateID by hand, backing POST
	// /templates/{id}/run.
	StartManual(ctx context.Context, actor Actor, templateID string, vars templates.Vars) (domain.Run, error)

	// GetLoop, ListLoops and Stop back the /loops routes.
	GetLoop(ctx context.Context, id string) (runs.LoopView, error)
	ListLoops(ctx context.Context, state string, limit int) ([]domain.Loop, error)
	Stop(ctx context.Context, actor Actor, id string) error
}

// SchedulesService is what internal/api's schedules handlers call. Card T46
// implements it as internal/schedules.Service; see TriggersService's doc
// comment for why this is an interface here.
type SchedulesService interface {
	Create(ctx context.Context, actor Actor, in domain.ScheduleInput) (domain.Schedule, error)
	List(ctx context.Context, actor Actor) ([]domain.Schedule, error)
	Get(ctx context.Context, actor Actor, id string) (domain.Schedule, error)
	Update(ctx context.Context, actor Actor, id string, in domain.ScheduleInput) (domain.Schedule, error)
	Delete(ctx context.Context, actor Actor, id string) error
	RunNow(ctx context.Context, actor Actor, id string) (domain.Run, error)
	Firings(ctx context.Context, actor Actor, id string, limit int) ([]domain.ScheduleFiring, error)
	// Preview validates a cron expression and returns its next few fire
	// times plus a human-readable description; it takes no actor since it
	// reads nothing schedule-specific.
	Preview(cronExpr string) (schedules.Preview, error)
}

// PipelinesService is what internal/api's pipelines and pipeline-runs
// handlers call. Card T56 implements it as internal/pipelines.Executor
// (CRUD + validation + execution); see TriggersService's doc comment for
// why this is an interface here.
type PipelinesService interface {
	CreatePipeline(ctx context.Context, actor Actor, in domain.PipelineInput) (domain.Pipeline, error)
	ListPipelines(ctx context.Context, actor Actor) ([]domain.Pipeline, error)
	GetPipeline(ctx context.Context, actor Actor, id string) (domain.Pipeline, error)
	UpdatePipeline(ctx context.Context, actor Actor, id string, in domain.PipelineInput) (domain.Pipeline, error)
	DeletePipeline(ctx context.Context, actor Actor, id string) error

	// Validate parses and validates yamlText against workspaceID's
	// templates without persisting anything, backing POST
	// /pipelines/validate. It only errors for a genuine service failure
	// (e.g. an unknown workspace) — an invalid definition is reported via
	// ValidationResult.OK/Problems, not an error.
	Validate(ctx context.Context, actor Actor, workspaceID string, yamlText []byte) (pipelines.ValidationResult, error)

	// Start begins a pipeline run, backing both POST /pipelines/{id}/start
	// (origin ui) and trigger/schedule delivery (origin webhook/schedule).
	Start(ctx context.Context, actor Actor, pipelineID string, input templates.Vars, origin domain.Origin, originRef string) (domain.PipelineRun, error)

	GetRun(ctx context.Context, actor Actor, id string) (pipelines.RunView, error)
	ListRuns(ctx context.Context, actor Actor, f domain.PipelineRunFilter) ([]domain.PipelineRun, error)
	Cancel(ctx context.Context, actor Actor, id string) error
	RetryFailed(ctx context.Context, actor Actor, id string) error
}

// StatsService is what internal/api's stats handlers call. Card T48
// implements it as internal/stats.Service; see TriggersService's doc
// comment for why this is an interface here.
type StatsService interface {
	Gantt(ctx context.Context, from, to time.Time, actor Actor) ([]stats.Lane, error)
	Costs(ctx context.Context, days int, actor Actor) (stats.Costs, error)
}

// Notifier sends one notification through a channel. The concrete
// implementation is *notify.Service; tests substitute a fake that records
// the call instead of making a real HTTP request.
type Notifier interface {
	Send(ctx context.Context, ch notify.Channel, ev notify.Event) error
}

// TokenVerifier checks that a credential actually works before Styr stores
// it. The real implementations run the CLI once (internal/harness/claude's
// and internal/harness/codex's Verifier); tests use a stub. The same
// interface serves the Claude token and the Codex API key — both are
// "spawn the CLI with this credential and see whether it is accepted".
type TokenVerifier interface {
	Verify(ctx context.Context, token string) error
}

// CodexCredentialStore is the subset of db.CodexCredentials the /me/codex-key
// and /settings/codex-key handlers need. Declared as an interface so a nil
// store (a build that never wired one) makes those routes answer 501 rather
// than panicking, matching TokenStore's rule above.
type CodexCredentialStore interface {
	Set(ctx context.Context, userID string, ciphertext, nonce []byte, label string) error
	Get(ctx context.Context, userID string) (ciphertext, nonce []byte, label string, addedAt time.Time, err error)
	Delete(ctx context.Context, userID string) error
	SetService(ctx context.Context, ciphertext, nonce []byte, label string) error
	GetService(ctx context.Context) (ciphertext, nonce []byte, label string, addedAt time.Time, err error)
}

// TokenStore is the subset of the api_tokens repository (T31's
// db.APITokens) the /me/api-tokens handlers need. Defined here, alongside
// auth.APITokenStore, rather than imported so this package does not depend
// on internal/db's concrete repository type; a nil TokenStore (the
// zero-value Deps, before T31's repository is wired in cmd/styr/wire.go)
// makes every /me/api-tokens handler answer 501 rather than panic.
type TokenStore interface {
	Create(ctx context.Context, t domain.APIToken) error
	ListByUser(ctx context.Context, userID string) ([]domain.APIToken, error)
	Delete(ctx context.Context, id, userID string) error
}

// HarnessInfo describes one agentic CLI this server was built to drive, as
// probed once at startup. Available is false when `<bin> --version` could not
// be run at all, which is how the UI explains a harness it must not offer.
type HarnessInfo struct {
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	// Bin is the configured binary name or path. It is the operator's own
	// config value, never a credential.
	Bin string `json:"bin"`
}

// StatusInfo is the payload for GET /api/v1/status.
type StatusInfo struct {
	Version       string `json:"version"`
	ClaudeVersion string `json:"claude_version"`
	OpenProcesses int    `json:"open_processes"`
	Slots         int    `json:"slots"`
	QueueDepth    int    `json:"queue_depth"`
	// Harnesses is every harness kind this build knows, with whether its
	// binary answered `--version` at startup and what it said.
	Harnesses []HarnessInfo `json:"harnesses"`
}

// Deps is everything the API handlers need.
type Deps struct {
	Auth *auth.Service
	// Workspaces is the workspaces service handlers call for every
	// workspace route; WorkspacesRepo is kept for callers (and tests) that
	// need direct repository access, e.g. to seed a workspace bypassing
	// service-level validation.
	Workspaces     *workspaces.Service
	WorkspacesRepo *db.Workspaces
	Sessions       *sessions.Service
	Users          *db.Users
	Tokens         *db.Tokens
	Profiles       *db.Profiles
	Audit          *db.Audit
	Bus            *events.Bus
	Box            *crypto.Box
	Verifier       TokenVerifier
	// CodexVerifier checks an OpenAI API key before it is stored, and
	// CodexCreds is where the sealed key goes. Both nil on a build without
	// the Codex harness wired in, which makes the codex-key routes answer
	// 501 instead of panicking.
	CodexVerifier TokenVerifier
	CodexCreds    CodexCredentialStore
	// TokenStore backs GET/POST /me/api-tokens and DELETE
	// /me/api-tokens/{id} (T36). nil until T31's db.APITokens repository is
	// wired in (cmd/styr/wire.go); the handlers answer 501 in that case
	// rather than panicking.
	TokenStore TokenStore
	Status     func() StatusInfo
	Version    string

	// Triggers and Runs back the templates/triggers/deliveries/runs/hooks
	// routes (internal/triggers.Service and internal/runs.Engine at
	// runtime, wired in by the composition root). Notifications is the
	// repository for notification_channels (a plain CRUD resource with no
	// per-user ownership, unlike Triggers' owned/shared resources).
	// Notifier sends the "test" notification for POST
	// /notifications/{id}/test.
	Triggers      TriggersService
	Runs          RunsEngine
	Notifications *db.NotificationChannels
	Notifier      Notifier

	// Schedules and Stats back the /schedules, /loops and /stats routes
	// (internal/schedules.Service and internal/stats.Service at runtime).
	Schedules SchedulesService
	Stats     StatsService

	// Pipelines backs the /pipelines and /pipeline-runs routes
	// (internal/pipelines.Executor at runtime).
	Pipelines PipelinesService

	// MaxOpenSessions and IdleTimeout surface the sessions scheduler's
	// configured limits on GET /api/v1/settings. sessions.Service does not
	// expose its Options (they are private to the scheduler), so the
	// composition root (Task 14) passes the same values it gave
	// sessions.New here.
	MaxOpenSessions int
	IdleTimeout     time.Duration
}
