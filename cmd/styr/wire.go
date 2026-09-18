package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/jonasthim/styr/internal/harness/claude"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/workspaces"
)

// probeVersionTimeout bounds the one-time `claude --version` call at
// startup used to populate the status endpoint.
const probeVersionTimeout = 10 * time.Second

// wireServices is Styr's composition root: it builds every repository and
// service from a loaded config, an already-open database and a shared event
// bus, and returns the Deps the HTTP router needs plus the sessions.Service
// serve.go drives directly (maintenance ticker, shutdown).
func wireServices(cfg config.Config, d *db.DB, bus *events.Bus) (*api.Deps, *sessions.Service, error) {
	box, err := crypto.NewBox(cfg.SecretKey)
	if err != nil {
		return nil, nil, fmt.Errorf("wire: %w", err)
	}

	users := db.NewUsers(d)
	tokens := db.NewTokens(d)
	logins := db.NewLoginSessions(d)
	workspacesRepo := db.NewWorkspaces(d)
	profiles := db.NewProfiles(d)
	sessionsRepo := db.NewSessions(d)
	eventsRepo := db.NewEvents(d)
	approvals := db.NewApprovals(d)
	audit := db.NewAudit(d)

	h := claude.New(cfg.ClaudeBin)

	workspacesSvc := workspaces.New(workspacesRepo, sessionsRepo, bus, cfg.UsersDir(), nil)

	svc := sessions.New(sessions.Repos{
		Sessions:   sessionsRepo,
		Events:     eventsRepo,
		Approvals:  approvals,
		Workspaces: workspacesRepo,
		Profiles:   profiles,
		Tokens:     tokens,
		Audit:      audit,
	}, h, bus, box, sessions.Options{
		MaxOpen:     cfg.MaxOpenSessions,
		IdleTimeout: cfg.IdleTimeout,
		UsersDir:    cfg.UsersDir(),
		ServiceHome: filepath.Join(cfg.UsersDir(), "__service__"),
	})

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
		return nil, nil, fmt.Errorf("wire: %w", err)
	}

	var verifier api.TokenVerifier
	if cfg.Env == "dev" {
		// The dev verifier never spawns a process, so the UI's token cards
		// can be exercised locally with any string shaped like a token,
		// without a real Claude credential or the shell fake understanding
		// --output-format json.
		verifier = devVerifier{}
	} else {
		verifier = claude.Verifier{Bin: cfg.ClaudeBin}
	}

	claudeVersion := probeClaudeVersion(cfg.ClaudeBin)

	deps := &api.Deps{
		Auth:           authSvc,
		Sessions:       svc,
		Users:          users,
		Tokens:         tokens,
		Workspaces:     workspacesSvc,
		WorkspacesRepo: workspacesRepo,
		Profiles:       profiles,
		Audit:          audit,
		Bus:            bus,
		Box:            box,
		Verifier:       verifier,
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
			}
		},
		MaxOpenSessions: cfg.MaxOpenSessions,
		IdleTimeout:     cfg.IdleTimeout,
	}
	return deps, svc, nil
}

// probeClaudeVersion runs `<bin> --version` once at startup and returns its
// trimmed stdout, or "unknown" when the binary cannot be run.
func probeClaudeVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeVersionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
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
