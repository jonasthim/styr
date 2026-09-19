package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	loadEnv(os.Stderr) // a no-op under systemd, which passes the env file itself
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

	// The pid file is how `styr backup`/`restore`/`doctor` tell, without a
	// network call or a database lock, whether a serve process is up
	// against this data_dir; it is removed again on every return path
	// below, including a signal-driven graceful shutdown.
	pidPath := pidFilePath(cfg.DataDir)
	if err := writePIDFile(pidPath); err != nil {
		log.Error("write pid file", "path", pidPath, "error", err)
		return 1
	}
	defer func() {
		if err := removePIDFile(pidPath); err != nil {
			log.Error("remove pid file", "path", pidPath, "error", err)
		}
	}()

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
	deps, sessionsSvc, bg, err := wireServices(cfg, d, bus)
	if err != nil {
		log.Error("wire services", "error", err)
		return 1
	}

	if cfg.Env == "dev" {
		log.Warn("dev mode is active: auth auto-login and the dev token verifier are enabled")
	}

	seedTemplates(context.Background(), deps, bg, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	maintCtx, maintCancel := context.WithCancel(context.Background())
	defer maintCancel()
	go runMaintenanceLoop(maintCtx, sessionsSvc, func(ctx context.Context) { seedTemplates(ctx, deps, bg, log) })
	// The run engine follows unattended sessions on the event bus and
	// closes out runs that overrun their timeout; it lives as long as the
	// server does. The scheduler ticks every 30s alongside it, firing
	// cron-driven runs through the same engine.
	go bg.Runs.Run(maintCtx)
	go bg.Schedules.Run(maintCtx)
	// The pipeline executor rides the same bus: it advances each pipeline
	// run's graph as the runs behind its steps finish, and sweeps pipeline
	// runs that overrun their timeout.
	go bg.Pipelines.Run(maintCtx)

	// Every request context derives from reqCtx so that cancelling it ends the
	// long-lived SSE streams; http.Server.Shutdown only waits for handlers and
	// would otherwise time out behind a stream that never finishes on its own.
	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.NewRouter(deps, web.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return reqCtx },
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
	reqCancel() // end SSE streams and any in-flight handler waits first
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
func seedTemplates(ctx context.Context, deps *api.Deps, bg *background, log *slog.Logger) {
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
		if err := bg.Triggers.EnsureSeeded(ctx, ws.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			log.Error("seed templates", "workspace_id", ws.ID, "error", err)
		}
		return
	}
	log.Debug("seed templates: no shared workspace to bind them to, skipping")
}

// runMaintenanceLoop calls svc.RunMaintenance and every extra task once at
// each tick until ctx is cancelled. Template seeding rides along because it
// is idempotent and a fresh install registers its first shared workspace
// long after boot; the seed then lands within a minute.
func runMaintenanceLoop(ctx context.Context, svc *sessions.Service, extra ...func(context.Context)) {
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			svc.RunMaintenance(ctx)
			for _, fn := range extra {
				fn(ctx)
			}
		}
	}
}
