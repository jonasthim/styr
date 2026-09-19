-- +goose NO TRANSACTION
-- +goose Up
-- A trigger or a schedule now starts either a template run or a pipeline
-- run (v0.5 "Executor": exactly one of template_id / pipeline_id).
-- Migration 00008 added the pipeline_id columns, but template_id was still
-- `NOT NULL REFERENCES templates(id)` from 00003/00006, so a row targeting a
-- pipeline had no legal value to put there. This relaxes both columns to
-- nullable, keeping the foreign key.
--
-- SQLite cannot drop a NOT NULL constraint in place, so each table is
-- rebuilt by SQLite's documented procedure: foreign keys off, copy into a
-- new table, drop the old one, rename the new one into its place (the
-- referencing tables — deliveries, runs, schedule_firings — name the table,
-- so they follow it). It runs outside a transaction because PRAGMA
-- foreign_keys is a no-op inside one; db.Open migrates on a single
-- connection, so every statement below shares that pragma.
PRAGMA foreign_keys = OFF;

CREATE TABLE triggers_new (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('generic','grafana','github')), secret_hash TEXT NOT NULL, secret_hint TEXT NOT NULL DEFAULT '',
  template_id TEXT REFERENCES templates(id), pipeline_id TEXT REFERENCES pipelines(id), enabled INTEGER NOT NULL DEFAULT 1,
  dedupe_key_template TEXT NOT NULL DEFAULT '', cooldown_s INTEGER NOT NULL DEFAULT 600, storm_cap_per_hour INTEGER NOT NULL DEFAULT 10,
  run_on_resolved INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_delivery_at TEXT);
INSERT INTO triggers_new (id, owner_user_id, name, slug, kind, secret_hash, secret_hint, template_id, pipeline_id, enabled,
  dedupe_key_template, cooldown_s, storm_cap_per_hour, run_on_resolved, created_at, updated_at, last_delivery_at)
  SELECT id, owner_user_id, name, slug, kind, secret_hash, secret_hint, template_id, pipeline_id, enabled,
  dedupe_key_template, cooldown_s, storm_cap_per_hour, run_on_resolved, created_at, updated_at, last_delivery_at FROM triggers;
DROP TABLE triggers;
ALTER TABLE triggers_new RENAME TO triggers;

CREATE TABLE schedules_new (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL,
  template_id TEXT REFERENCES templates(id), pipeline_id TEXT REFERENCES pipelines(id), cron TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  vars TEXT NOT NULL DEFAULT '{}', last_run_at TEXT, last_outcome TEXT NOT NULL DEFAULT '', next_run_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
INSERT INTO schedules_new (id, owner_user_id, name, template_id, pipeline_id, cron, enabled, vars,
  last_run_at, last_outcome, next_run_at, created_at, updated_at)
  SELECT id, owner_user_id, name, template_id, pipeline_id, cron, enabled, vars,
  last_run_at, last_outcome, next_run_at, created_at, updated_at FROM schedules;
DROP TABLE schedules;
ALTER TABLE schedules_new RENAME TO schedules;
CREATE INDEX schedules_next_run_idx ON schedules(enabled, next_run_at);

PRAGMA foreign_keys = ON;

-- +goose Down
-- Rolling back drops the rows that target a pipeline: the old schema has
-- nowhere to put them.
PRAGMA foreign_keys = OFF;

DELETE FROM triggers WHERE template_id IS NULL;
CREATE TABLE triggers_old (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('generic','grafana','github')), secret_hash TEXT NOT NULL, secret_hint TEXT NOT NULL DEFAULT '',
  template_id TEXT NOT NULL REFERENCES templates(id), pipeline_id TEXT REFERENCES pipelines(id), enabled INTEGER NOT NULL DEFAULT 1,
  dedupe_key_template TEXT NOT NULL DEFAULT '', cooldown_s INTEGER NOT NULL DEFAULT 600, storm_cap_per_hour INTEGER NOT NULL DEFAULT 10,
  run_on_resolved INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_delivery_at TEXT);
INSERT INTO triggers_old (id, owner_user_id, name, slug, kind, secret_hash, secret_hint, template_id, pipeline_id, enabled,
  dedupe_key_template, cooldown_s, storm_cap_per_hour, run_on_resolved, created_at, updated_at, last_delivery_at)
  SELECT id, owner_user_id, name, slug, kind, secret_hash, secret_hint, template_id, pipeline_id, enabled,
  dedupe_key_template, cooldown_s, storm_cap_per_hour, run_on_resolved, created_at, updated_at, last_delivery_at FROM triggers;
DROP TABLE triggers;
ALTER TABLE triggers_old RENAME TO triggers;

DELETE FROM schedules WHERE template_id IS NULL;
CREATE TABLE schedules_old (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL,
  template_id TEXT NOT NULL REFERENCES templates(id), pipeline_id TEXT REFERENCES pipelines(id), cron TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  vars TEXT NOT NULL DEFAULT '{}', last_run_at TEXT, last_outcome TEXT NOT NULL DEFAULT '', next_run_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
INSERT INTO schedules_old (id, owner_user_id, name, template_id, pipeline_id, cron, enabled, vars,
  last_run_at, last_outcome, next_run_at, created_at, updated_at)
  SELECT id, owner_user_id, name, template_id, pipeline_id, cron, enabled, vars,
  last_run_at, last_outcome, next_run_at, created_at, updated_at FROM schedules;
DROP TABLE schedules;
ALTER TABLE schedules_old RENAME TO schedules;
CREATE INDEX schedules_next_run_idx ON schedules(enabled, next_run_at);

PRAGMA foreign_keys = ON;
