package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/claude"
	"github.com/jonasthim/styr/internal/harness/codex"
	"github.com/jonasthim/styr/internal/notify"
	"github.com/jonasthim/styr/internal/pipelines"
	"github.com/jonasthim/styr/internal/runs"
	"github.com/jonasthim/styr/internal/schedules"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/stats"
	"github.com/jonasthim/styr/internal/triggers"
	"github.com/jonasthim/styr/internal/workspaces"
)

// probeVersionTimeout bounds the one-time `<bin> --version` call made per
// harness at startup to populate the status endpoint.
const probeVersionTimeout = 10 * time.Second

// runTimeout is how long an unattended run may stay running before the run
// engine closes it out as timed out. Not configurable yet: the v0.2 plan
// fixes it at 30 minutes, matching the unattended profiles' approval
// timeout budget.
const runTimeout = 30 * time.Minute

// notifyTimeout bounds one outbound notification HTTP attempt; notify
// applies its own per-attempt timeout on top, this is the client ceiling.
const notifyTimeout = 30 * time.Second

// wireServices is Styr's composition root: it builds every repository and
// service from a loaded config, an already-open database and a shared event
// bus, and returns the Deps the HTTP router needs plus the sessions.Service
// serve.go drives directly (maintenance ticker, shutdown).
// background holds the concrete services serve needs beyond the API deps:
// the run engine's loop, the trigger service's seeding and the scheduler's
// tick.
type background struct {
	Runs      *runs.Engine
	Triggers  *triggers.Service
	Schedules *schedules.Service
	Pipelines *pipelines.Executor
}

func wireServices(cfg config.Config, d *db.DB, bus *events.Bus) (*api.Deps, *sessions.Service, *background, error) {
	box, err := crypto.NewBox(cfg.SecretKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("wire: %w", err)
	}

	users := db.NewUsers(d)
	tokens := db.NewTokens(d)
	codexCreds := db.NewCodexCredentials(d)
	logins := db.NewLoginSessions(d)
	workspacesRepo := db.NewWorkspaces(d)
	workspaceAccessRepo := db.NewWorkspaceAccess(d)
	profiles := db.NewProfiles(d)
	sessionsRepo := db.NewSessions(d)
	eventsRepo := db.NewEvents(d)
	approvals := db.NewApprovals(d)
	audit := db.NewAudit(d)
	templatesRepo := db.NewTemplates(d)
	triggersRepo := db.NewTriggers(d)
	deliveriesRepo := db.NewDeliveries(d)
	runsRepo := db.NewRuns(d)
	channelsRepo := db.NewNotificationChannels(d)
	reviewCommentsRepo := db.NewReviewComments(d)
	checkpointsRepo := db.NewCheckpoints(d)
	loopsRepo := db.NewLoops(d)
	schedulesRepo := db.NewSchedules(d)
	scheduleFiringsRepo := db.NewScheduleFirings(d)
	pipelinesRepo := db.NewPipelines(d)
	pipelineRunsRepo := db.NewPipelineRuns(d)
	stepRunsRepo := db.NewStepRuns(d)

	// Every harness this build can drive, registered by kind: a session picks one on its own
	// row (its profile's default, or the request's), and internal/sessions looks it up again
	// on every resume.
	registry := harness.New()
	registry.Register(claude.New(cfg.ClaudeBin))
	registry.Register(codex.New(cfg.CodexBin))

	workspacesSvc := workspaces.New(workspacesRepo, sessionsRepo, workspaceAccessRepo, bus, cfg.UsersDir(), nil)

	svc := sessions.New(sessions.Repos{
		Sessions:        sessionsRepo,
		Events:          eventsRepo,
		Approvals:       approvals,
		Workspaces:      workspacesRepo,
		WorkspaceAccess: workspaceAccessRepo,
		Profiles:        profiles,
		Tokens:          tokens,
		Audit:           audit,
		ReviewComments:  reviewCommentsRepo,
		Checkpoints:     checkpointsRepo,
		Users:           users,

		CodexCredentials: codexCreds,
	}, registry, bus, box, sessions.Options{
		MaxOpen:     cfg.MaxOpenSessions,
		IdleTimeout: cfg.IdleTimeout,
		UsersDir:    cfg.UsersDir(),
		ServiceHome: filepath.Join(cfg.UsersDir(), "__service__"),
	})

	// The run engine turns a rendered template into an unattended session
	// and follows it to an outcome; the trigger router is the inbound side
	// that feeds it. notify is the outbound side both share.
	notifier := notify.New(&http.Client{Timeout: notifyTimeout}, slog.Default())
	runsEngine := runs.New(runs.Repos{
		Runs:       runsRepo,
		Templates:  templatesRepo,
		Sessions:   sessionsRepo,
		Channels:   channelsRepo,
		Deliveries: deliveriesRepo,
		Loops:      loopsRepo,
		Events:     eventsRepo,
		// A run a pipeline step started resolves back to that step, its
		// pipeline run and its pipeline, for GET /runs/{id}'s view.
		StepRuns:     stepRunsRepo,
		PipelineRuns: pipelineRunsRepo,
		Pipelines:    pipelinesRepo,
	}, svc, bus, notifier, box, cfg.BaseURL, runTimeout, slog.Default())
	triggersSvc := triggers.New(triggers.Repos{
		Templates:  templatesRepo,
		Triggers:   triggersRepo,
		Deliveries: deliveriesRepo,
	}, runsEngine, box, slog.Default(), nil)

	// The scheduler ticks schedules through the same run engine every 30s;
	// runsEngine satisfies both schedules.RunStarter (Start) and
	// schedules.RunLookup (Get), so a schedule's overlap check reads the
	// same run rows the API and the trigger router do.
	schedulesSvc := schedules.New(schedules.Repos{
		Schedules: schedulesRepo,
		Firings:   scheduleFiringsRepo,
	}, runsEngine, runsEngine, nil, slog.Default())

	// The pipeline executor chains runs: it starts each step through the
	// same run engine and advances the graph on the event bus. Triggers and
	// schedules learn about it afterwards, since it is only reachable once
	// the run engine exists.
	pipelinesExec := pipelines.New(pipelines.Repos{
		Pipelines:    pipelinesRepo,
		PipelineRuns: pipelineRunsRepo,
		StepRuns:     stepRunsRepo,
		Runs:         runsRepo,
		Workspaces:   workspacesRepo,
	}, runsEngine, svc, sessionsRepo, bus, pipelines.NewTemplateIndex(templatesRepo), nil, slog.Default())
	triggersSvc = triggersSvc.WithPipelines(pipelinesExec)
	// WithPipelineRuns as well as WithPipelines: the scheduler needs the
	// executor to start a pipeline, and to read the "pr:" reference back
	// when deciding whether the previous firing is still going.
	schedulesSvc = schedulesSvc.WithPipelines(pipelinesExec).WithPipelineRuns(pipelinesExec)

	statsSvc := stats.New(d, sessionsRepo, users, slog.Default())

	// devUser only enables the dev auto-login bypass when the server is
	// actually running in dev mode, even if a stale dev_user lingers in a
	// prod config file.
	devUser := ""
	if cfg.Env == "dev" {
		devUser = cfg.DevUser
	}
	secure := strings.HasPrefix(cfg.BaseURL, "https://")
	authSvc, err := auth.New(users, logins, cfg.OIDC, cfg.BaseURL, secure, devUser)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("wire: %w", err)
	}

	// internal/db/api_tokens.go (db.NewAPITokens(d) *db.APITokens,
	// api_tokens table migration, in a parallel worktree — neither exists
	// in this one, so the store stays nil for now. With it nil,
	// auth.Service never matches a bearer token (falls through exactly
	// like a missing cookie, see auth.Service.principalFromBearer) and
	// GET/POST/DELETE /me/api-tokens answer 501 "not_implemented" rather
	//
	//
	//
	// db.NewAPITokens never returns nil, so no extra nil check is needed
	// around those two lines; the nil-safety already lives entirely in
	apiTokensRepo := db.NewAPITokens(d)
	authSvc = authSvc.WithAPITokens(apiTokensRepo)

	var verifier api.TokenVerifier
	var codexVerifier api.TokenVerifier
	if cfg.Env == "dev" {
		// The dev verifiers never spawn a process, so the UI's credential cards can be
		// exercised locally with any string shaped like a token or an API key, without a real
		// Claude or OpenAI credential and without the shell fakes having to understand
		// --output-format json.
		verifier = devVerifier{}
		codexVerifier = devCodexVerifier{}
	} else {
		verifier = claude.Verifier{Bin: cfg.ClaudeBin}
		codexVerifier = codex.Verifier{Bin: cfg.CodexBin}
	}

	claudeVersion := probeVersion(cfg.ClaudeBin)
	harnessInfos := []api.HarnessInfo{
		harnessInfo(harness.KindClaude, cfg.ClaudeBin, claudeVersion),
		harnessInfo(harness.KindCodex, cfg.CodexBin, probeVersion(cfg.CodexBin)),
	}

	deps := &api.Deps{
		TokenStore:     apiTokensRepo,
		Auth:           authSvc,
		Sessions:       svc,
		Triggers:       triggersSvc,
		Runs:           runsEngine,
		Notifications:  channelsRepo,
		Notifier:       notifier,
		Schedules:      schedulesSvc,
		Pipelines:      pipelinesExec,
		Stats:          statsSvc,
		Users:          users,
		Tokens:         tokens,
		Workspaces:     workspacesSvc,
		WorkspacesRepo: workspacesRepo,
		Profiles:       profiles,
		Audit:          audit,
		Bus:            bus,
		Box:            box,
		Verifier:       verifier,
		CodexVerifier:  codexVerifier,
		CodexCreds:     codexCreds,
		Version:        version,
		Status: func() api.StatusInfo {
			// sessions.Service tracks its live processes privately and
			// exposes no counters, so OpenProcesses and QueueDepth are
			// reported as zero here rather than guessed at; Slots reflects
			// the configured limit, which is accurate regardless.
			return api.StatusInfo{
				Version:       version,
				ClaudeVersion: claudeVersion,
				OpenProcesses: 0,
				Slots:         cfg.MaxOpenSessions,
				QueueDepth:    0,
				Harnesses:     harnessInfos,
			}
		},
		MaxOpenSessions: cfg.MaxOpenSessions,
		IdleTimeout:     cfg.IdleTimeout,
	}
	return deps, svc, &background{Runs: runsEngine, Triggers: triggersSvc, Schedules: schedulesSvc, Pipelines: pipelinesExec}, nil
}

// probeVersion runs `<bin> --version` once at startup and returns its
// trimmed stdout, or "unknown" when the binary cannot be run.
func probeVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeVersionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return firstLine(string(out))
}

// firstLine is the first non-empty line of s, capped at 80 characters. A real
// CLI answers --version with exactly one short line; a shell fake standing in
// for one during tests may answer with whatever it prints by default, and that
// belongs nowhere near a status field the UI renders.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			if len(l) > 80 {
				return l[:80]
			}
			return l
		}
	}
	return ""
}

// harnessInfo turns a startup version probe into the entry GET /status
// reports: a harness whose binary could not be run at all is listed as
// unavailable rather than hidden, so the UI can say why a choice is missing
// instead of silently omitting it.
func harnessInfo(kind harness.Kind, bin, version string) api.HarnessInfo {
	return api.HarnessInfo{
		Kind:      string(kind),
		Available: version != "unknown",
		Version:   version,
		Bin:       bin,
	}
}

// devVerifier is wired instead of claude.Verifier when cfg.Env == "dev". It
// accepts anything shaped like a Claude token (the "sk-ant-" prefix every
// real token and OAuth credential shares) without spawning a process, and
// rejects everything else so the profile UI's error path stays exercisable.
type devVerifier struct{}

func (devVerifier) Verify(_ context.Context, token string) error {
	if strings.HasPrefix(token, "sk-ant-") {
		return nil
	}
	return errors.New("claude: dev verifier: token must start with sk-ant-")
}

// devCodexVerifier is wired instead of codex.Verifier when cfg.Env == "dev".
// It accepts anything shaped like an OpenAI API key (the "sk-" prefix)
// without spawning a process, and rejects everything else so the profile
// UI's error path stays exercisable.
type devCodexVerifier struct{}

func (devCodexVerifier) Verify(_ context.Context, key string) error {
	if strings.HasPrefix(key, "sk-") {
		return nil
	}
	return errors.New("codex: dev verifier: key must start with sk-")
}
