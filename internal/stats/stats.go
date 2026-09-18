// Package stats computes read-only fleet views over the sessions, events and
// runs tables: a per-session Gantt of running/waiting/idle time (Service.Gantt)
// and cost aggregation by day, owner, origin and template (Service.Costs). It
// never writes to the database; every query here is a plain SELECT.
package stats

import (
	"log/slog"

	"github.com/jonasthim/styr/internal/db"
)

// Service computes stats views. It holds the shared *db.DB for the ad-hoc
// read-only SQL Gantt and Costs need (events in a time window, a runs/
// templates join for top templates) plus the Sessions and Users repositories
// for visibility-aware session listing and owner display-name lookups.
type Service struct {
	db           *db.DB
	sessionsRepo *db.Sessions
	users        *db.Users
	logger       *slog.Logger
}

// New constructs a Service. logger defaults to slog.Default() when nil.
func New(d *db.DB, sessionsRepo *db.Sessions, users *db.Users, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{db: d, sessionsRepo: sessionsRepo, users: users, logger: logger}
}
