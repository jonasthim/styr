-- +goose Up
-- Schedules: cron-driven template runs, ticked by internal/schedules every
-- 30s. A schedule never overlaps itself (see schedule_firings.status).
CREATE TABLE schedules (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL,
  template_id TEXT NOT NULL REFERENCES templates(id), cron TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  vars TEXT NOT NULL DEFAULT '{}', last_run_at TEXT, last_outcome TEXT NOT NULL DEFAULT '', next_run_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE INDEX schedules_next_run_idx ON schedules(enabled, next_run_at);
CREATE TABLE schedule_firings (
  id TEXT PRIMARY KEY, schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE, fired_at TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('started','skipped_overlap','failed')), reason TEXT NOT NULL DEFAULT '', run_id TEXT);
CREATE INDEX schedule_firings_schedule_idx ON schedule_firings(schedule_id, fired_at DESC);

-- +goose Down
DROP TABLE schedule_firings;
DROP TABLE schedules;
