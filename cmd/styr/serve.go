package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/web"
)

// maintenanceInterval is how often RunMaintenance runs while the server is
// up (idle-session reaping, stale-approval expiry).
const maintenanceInterval = time.Minute

// shutdownTimeout bounds both the HTTP server's graceful shutdown and the
// sessions service's process shutdown.
const shutdownTimeout = 15 * time.Second

// runServe loads the configuration, opens the database, wires every
// service, and serves the API and embedded SPA until it receives SIGINT or
// SIGTERM. All logging goes to stderr; stdout is reserved for a fatal
// startup error message, matching the other subcommands.
func runServe(stdout io.Writer) int {
	cfg, err := config.Load(os.Getenv("STYR_CONFIG"))
	if err != nil {
		fmt.Fprintf(stdout, "serve: %v\n", err)
		return 1
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		log.Error("create data dir", "dir", cfg.DataDir, "error", err)
		return 1
	}

	d, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Error("open database", "error", err)
		return 1
	}
	defer func() { _ = d.Close() }()

	// A process cannot survive a restart, so any session this database
	// still lists as open, running or waiting belongs to a process that is
	// gone. There is no sessions repo method for this (it is a one-time
	// startup fixup, not a service operation), so it runs as a direct SQL
	// statement against the embedded *sql.DB here.
	if _, err := d.ExecContext(context.Background(),
		`UPDATE sessions SET state = 'closed' WHERE state IN ('open', 'running', 'waiting')`,
	); err != nil {
		log.Error("reset stale sessions", "error", err)
		return 1
	}

	bus := events.New()
	deps, sessionsSvc, err := wireServices(cfg, d, bus)
	if err != nil {
		log.Error("wire services", "error", err)
		return 1
	}

	if cfg.Env == "dev" {
		log.Warn("dev mode is active: auth auto-login and the dev token verifier are enabled")
	}

	seedTemplates(context.Background(), deps, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	maintCtx, maintCancel := context.WithCancel(context.Background())
	defer maintCancel()
	go runMaintenanceLoop(maintCtx, sessionsSvc)
	// The run engine follows unattended sessions on the event bus and
	// closes out runs that overrun their timeout; it lives as long as the
	// server does.
	go deps.Runs.Run(maintCtx)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.NewRouter(deps, web.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "env", cfg.Env, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			log.Error("listen", "error", err)
			return 1
		}
		return 0
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error("http server shutdown", "error", err)
	}

	sessCtx, sessCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer sessCancel()
	if err := sessionsSvc.Shutdown(sessCtx); err != nil {
		log.Error("sessions shutdown", "error", err)
	}
	log.Info("shutdown complete")
	return 0
}

// seedTemplates creates the shipped trigger templates once, bound to the
// first shared (owner-less) workspace. With no shared workspace there is
// nothing sensible to bind them to, so seeding is skipped: an admin
// registering one later can create the template from the UI.
func seedTemplates(ctx context.Context, deps *api.Deps, log *slog.Logger) {
	// An empty user id with no admin flag lists exactly the shared
	// workspaces.
	shared, err := deps.WorkspacesRepo.ListVisible(ctx, "", false)
	if err != nil {
		log.Error("seed templates: list shared workspaces", "error", err)
		return
	}
	for _, ws := range shared {
		if ws.OwnerID != nil {
			continue
		}
		if err := deps.Triggers.EnsureSeeded(ctx, ws.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			log.Error("seed templates", "workspace_id", ws.ID, "error", err)
		}
		return
	}
	log.Info("seed templates: no shared workspace to bind them to, skipping")
}

// runMaintenanceLoop calls svc.RunMaintenance once at each tick until ctx is
// cancelled.
func runMaintenanceLoop(ctx context.Context, svc *sessions.Service) {
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			svc.RunMaintenance(ctx)
		}
	}
}
