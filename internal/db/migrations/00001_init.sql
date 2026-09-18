-- +goose Up
CREATE TABLE users (
  id TEXT PRIMARY KEY, issuer TEXT NOT NULL, subject TEXT NOT NULL, email TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '', avatar_url TEXT NOT NULL DEFAULT '', role TEXT NOT NULL CHECK (role IN ('admin','member')),
  created_at TEXT NOT NULL, last_login_at TEXT NOT NULL, prefs TEXT NOT NULL DEFAULT '{}',
  UNIQUE (issuer, subject)
);
CREATE TABLE claude_tokens (
  user_id TEXT PRIMARY KEY, ciphertext BLOB NOT NULL, nonce BLOB NOT NULL, label TEXT NOT NULL DEFAULT '',
  added_at TEXT NOT NULL, verified_at TEXT
);
CREATE TABLE login_sessions (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, expires_at TEXT NOT NULL, user_agent TEXT NOT NULL DEFAULT ''
);
CREATE TABLE profiles (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, mode TEXT NOT NULL, allowed_tools TEXT NOT NULL DEFAULT '[]',
  disallowed_tools TEXT NOT NULL DEFAULT '[]', max_turns INTEGER NOT NULL DEFAULT 0, unattended INTEGER NOT NULL DEFAULT 0,
  approval_timeout_s INTEGER NOT NULL DEFAULT 1800, builtin INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE workspaces (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, path TEXT NOT NULL, default_profile_id TEXT NOT NULL REFERENCES profiles(id),
  worktrees INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL
);
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL, title TEXT NOT NULL,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id), profile_id TEXT NOT NULL REFERENCES profiles(id), harness TEXT NOT NULL,
  state TEXT NOT NULL, origin TEXT NOT NULL, origin_ref TEXT NOT NULL DEFAULT '', worktree TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, last_active_at TEXT NOT NULL, num_turns INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0,
  tokens_in INTEGER NOT NULL DEFAULT 0, tokens_out INTEGER NOT NULL DEFAULT 0, now_line TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_owner_idx ON sessions(owner_user_id, last_active_at DESC);
CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL, at TEXT NOT NULL, type TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE (session_id, seq)
);
CREATE TABLE approvals (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, request_id TEXT NOT NULL,
  tool TEXT NOT NULL, input TEXT NOT NULL, risk TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL,
  decided_by TEXT, decided_at TEXT, snoozed_until TEXT, updated_input TEXT, message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX approvals_pending_idx ON approvals(state, created_at);
CREATE TABLE audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL, actor TEXT NOT NULL, action TEXT NOT NULL, target TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '{}'
);
INSERT INTO profiles (id, name, mode, allowed_tools, disallowed_tools, max_turns, unattended, approval_timeout_s, builtin) VALUES
 ('interactive','interactive','default','[]','[]',0,0,0,1),
 ('investigate','investigate','default','["Read","Grep","Glob","Bash(ssh * journalctl *)","Bash(ssh * systemctl status *)","Bash(curl *)"]','["WebFetch","Edit","Write"]',30,1,1800,1),
 ('remediate','remediate','default','["Read","Grep","Glob","Bash(ssh * journalctl *)","Bash(ssh * systemctl status *)","Bash(ssh * systemctl restart *)","Bash(ssh * pct restart *)","Bash(curl *)"]','["WebFetch"]',40,1,1800,1);
-- +goose Down
DROP TABLE audit; DROP TABLE approvals; DROP TABLE events; DROP TABLE sessions; DROP TABLE workspaces; DROP TABLE profiles; DROP TABLE login_sessions; DROP TABLE claude_tokens; DROP TABLE users;
