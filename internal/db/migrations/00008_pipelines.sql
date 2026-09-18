-- +goose Up
-- Pipelines: a small YAML DAG of steps, each backed by a template run. See
-- docs/superpowers/plans/2026-09-19-styr-v0.5-pipelines.md ("YAML
-- definition", "Data model") for the shape parsed and validated by
-- internal/pipelines. A pipeline run creates one step_runs row per node
-- (fan-out creates N); a retried attempt of a node gets a new step_runs
-- row (same step_id, incremented attempt) rather than mutating the failed
-- one, so every attempt's report stays on its own row.
CREATE TABLE pipelines (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  yaml TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX pipelines_owner_name ON pipelines(COALESCE(owner_user_id,''), name);
CREATE TABLE pipeline_runs (
  id TEXT PRIMARY KEY, pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE, origin TEXT NOT NULL, origin_ref TEXT NOT NULL DEFAULT '',
  input TEXT NOT NULL DEFAULT '{}', state TEXT NOT NULL CHECK (state IN ('running','success','failed','cancelled','timeout')),
  started_at TEXT NOT NULL, finished_at TEXT, cost_usd REAL NOT NULL DEFAULT 0);
CREATE TABLE step_runs (
  id TEXT PRIMARY KEY, pipeline_run_id TEXT NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE, step_id TEXT NOT NULL, index_in_fanout INTEGER NOT NULL DEFAULT 0,
  item TEXT NOT NULL DEFAULT '', run_id TEXT REFERENCES runs(id), attempt INTEGER NOT NULL DEFAULT 1,
  state TEXT NOT NULL CHECK (state IN ('pending','running','success','failed','skipped','cancelled')), report TEXT NOT NULL DEFAULT '',
  started_at TEXT, finished_at TEXT, worktree TEXT NOT NULL DEFAULT '');
CREATE INDEX step_runs_pipeline_idx ON step_runs(pipeline_run_id);
ALTER TABLE triggers ADD COLUMN pipeline_id TEXT REFERENCES pipelines(id);
ALTER TABLE schedules ADD COLUMN pipeline_id TEXT REFERENCES pipelines(id);
ALTER TABLE runs ADD COLUMN step_run_id TEXT REFERENCES step_runs(id);

-- +goose Down
ALTER TABLE runs DROP COLUMN step_run_id;
ALTER TABLE schedules DROP COLUMN pipeline_id;
ALTER TABLE triggers DROP COLUMN pipeline_id;
DROP INDEX step_runs_pipeline_idx;
DROP TABLE step_runs;
DROP TABLE pipeline_runs;
DROP INDEX pipelines_owner_name;
DROP TABLE pipelines;
