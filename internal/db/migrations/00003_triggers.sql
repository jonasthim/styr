-- +goose Up
CREATE TABLE templates (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id), profile_id TEXT NOT NULL REFERENCES profiles(id),
  title_template TEXT NOT NULL DEFAULT '', prompt_template TEXT NOT NULL, system_prompt TEXT NOT NULL DEFAULT '',
  report_schema TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE UNIQUE INDEX templates_owner_name ON templates(COALESCE(owner_user_id,''), name);
CREATE TABLE triggers (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('generic','grafana','github')), secret_hash TEXT NOT NULL, secret_hint TEXT NOT NULL DEFAULT '',
  template_id TEXT NOT NULL REFERENCES templates(id), enabled INTEGER NOT NULL DEFAULT 1,
  dedupe_key_template TEXT NOT NULL DEFAULT '', cooldown_s INTEGER NOT NULL DEFAULT 600, storm_cap_per_hour INTEGER NOT NULL DEFAULT 10,
  run_on_resolved INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_delivery_at TEXT);
CREATE TABLE deliveries (
  id TEXT PRIMARY KEY, trigger_id TEXT NOT NULL REFERENCES triggers(id) ON DELETE CASCADE, received_at TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('accepted','deduped','cooldown','storm','rejected','failed','skipped')),
  reason TEXT NOT NULL DEFAULT '', dedupe_key TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL, run_id TEXT);
CREATE INDEX deliveries_trigger_idx ON deliveries(trigger_id, received_at DESC);
CREATE TABLE runs (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, template_id TEXT REFERENCES templates(id),
  trigger_id TEXT REFERENCES triggers(id), delivery_id TEXT REFERENCES deliveries(id), origin TEXT NOT NULL,
  started_at TEXT NOT NULL, finished_at TEXT, outcome TEXT NOT NULL CHECK (outcome IN ('running','success','failed','timeout','needs_human')),
  report TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', cost_usd REAL NOT NULL DEFAULT 0);
CREATE INDEX runs_started_idx ON runs(started_at DESC);
CREATE TABLE notification_channels (
  id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('ntfy','webhook')), name TEXT NOT NULL, url TEXT NOT NULL,
  token_ciphertext BLOB, token_nonce BLOB, events TEXT NOT NULL DEFAULT '["run.finished","run.needs_human","run.failed"]',
  enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE api_tokens (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL, created_at TEXT NOT NULL, last_used_at TEXT, expires_at TEXT);

-- +goose Down
DROP TABLE api_tokens;
DROP TABLE notification_channels;
DROP TABLE runs;
DROP TABLE deliveries;
DROP TABLE triggers;
DROP TABLE templates;
