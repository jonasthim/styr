-- +goose NO TRANSACTION
-- +goose Up
-- T64: viewer role and per-workspace access lists.
--
-- The users.role CHECK constraint only allowed 'admin'/'member', so adding
-- 'viewer' needs SQLite's documented rebuild procedure (foreign keys off,
-- copy into a new table, drop the old one, rename the new one into its
-- place — same as migration 00010's trigger/schedule rebuild). Every other
-- table's REFERENCES users(id) names the table by name, so it keeps working
-- once the rebuilt table is renamed back to "users" — nothing referencing
-- it needs to change.
--
-- A shared (owner-less) workspace's access defaults to "everyone" (every
-- signed-in user can see and use it, exactly as before this migration); an
-- admin can switch it to "listed" and name the users in workspace_access
-- who may then see and use it (an admin can always see and use it,
-- regardless of the list). Adding that column with its own CHECK
-- constraint is a plain ALTER TABLE ADD COLUMN — SQLite allows a CHECK on
-- an added column as long as it has a constant DEFAULT, unlike the
-- users.role rebuild above.
PRAGMA foreign_keys = OFF;

CREATE TABLE users_new (
  id TEXT PRIMARY KEY, issuer TEXT NOT NULL, subject TEXT NOT NULL, email TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '', avatar_url TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL CHECK (role IN ('admin','member','viewer')),
  created_at TEXT NOT NULL, last_login_at TEXT NOT NULL, prefs TEXT NOT NULL DEFAULT '{}',
  UNIQUE (issuer, subject)
);
INSERT INTO users_new (id, issuer, subject, email, display_name, avatar_url, role, created_at, last_login_at, prefs)
  SELECT id, issuer, subject, email, display_name, avatar_url, role, created_at, last_login_at, prefs FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

CREATE TABLE workspace_access (
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (workspace_id, user_id)
);

ALTER TABLE workspaces ADD COLUMN access TEXT NOT NULL DEFAULT 'everyone' CHECK (access IN ('everyone','listed'));

PRAGMA foreign_keys = ON;

-- +goose Down
-- Rolling back drops any viewer users' rows: the old schema's CHECK
-- constraint has no legal role to put them in.
PRAGMA foreign_keys = OFF;

ALTER TABLE workspaces DROP COLUMN access;
DROP TABLE workspace_access;

DELETE FROM users WHERE role = 'viewer';
CREATE TABLE users_old (
  id TEXT PRIMARY KEY, issuer TEXT NOT NULL, subject TEXT NOT NULL, email TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '', avatar_url TEXT NOT NULL DEFAULT '', role TEXT NOT NULL CHECK (role IN ('admin','member')),
  created_at TEXT NOT NULL, last_login_at TEXT NOT NULL, prefs TEXT NOT NULL DEFAULT '{}',
  UNIQUE (issuer, subject)
);
INSERT INTO users_old (id, issuer, subject, email, display_name, avatar_url, role, created_at, last_login_at, prefs)
  SELECT id, issuer, subject, email, display_name, avatar_url, role, created_at, last_login_at, prefs FROM users;
DROP TABLE users;
ALTER TABLE users_old RENAME TO users;

PRAGMA foreign_keys = ON;
