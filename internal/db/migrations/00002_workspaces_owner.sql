-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys=OFF;

CREATE TABLE workspaces_new (
  id TEXT PRIMARY KEY,
  owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  default_profile_id TEXT NOT NULL REFERENCES profiles(id),
  worktrees INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT 'path',
  repo_url TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  managed INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'ready',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT ''
);

INSERT INTO workspaces_new (id, owner_user_id, name, path, default_profile_id, worktrees, source, repo_url, branch, managed, state, error, created_at, updated_at)
SELECT id, NULL, name, path, default_profile_id, worktrees, 'path', '', '', 0, 'ready', '', created_at, created_at
FROM workspaces;

DROP TABLE workspaces;
ALTER TABLE workspaces_new RENAME TO workspaces;

CREATE UNIQUE INDEX workspaces_owner_name ON workspaces(COALESCE(owner_user_id, ''), name);

PRAGMA foreign_keys=ON;

-- +goose Down
PRAGMA foreign_keys=OFF;

CREATE TABLE workspaces_old (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, path TEXT NOT NULL, default_profile_id TEXT NOT NULL REFERENCES profiles(id),
  worktrees INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL
);

INSERT INTO workspaces_old (id, name, path, default_profile_id, worktrees, created_at)
SELECT id, name, path, default_profile_id, worktrees, created_at FROM workspaces;

DROP TABLE workspaces;
ALTER TABLE workspaces_old RENAME TO workspaces;

PRAGMA foreign_keys=ON;
