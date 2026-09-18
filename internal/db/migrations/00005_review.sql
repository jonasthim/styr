-- +goose Up
-- Review: a git worktree per session, checkpoints to rewind to, inline diff
-- comments that become the next prompt, and the plan markdown an ExitPlanMode
-- approval carries.
ALTER TABLE workspaces ADD COLUMN base_branch TEXT NOT NULL DEFAULT '';      -- '' = detect (the repo's branch at worktree creation)
ALTER TABLE workspaces ADD COLUMN auto_checkpoint INTEGER NOT NULL DEFAULT 1;

ALTER TABLE sessions ADD COLUMN branch TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN base_ref TEXT NOT NULL DEFAULT '';           -- commit the worktree started from
ALTER TABLE sessions ADD COLUMN diff_add INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN diff_del INTEGER NOT NULL DEFAULT 0;

-- The plan markdown an ExitPlanMode permission request carries, so the inbox
-- and session view can render the plan card without a live process.
ALTER TABLE approvals ADD COLUMN plan TEXT NOT NULL DEFAULT '';

CREATE TABLE review_comments (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  line INTEGER NOT NULL,
  side TEXT NOT NULL CHECK (side IN ('old','new')),
  body TEXT NOT NULL,
  author_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  sent_at TEXT
);
CREATE INDEX review_comments_session_idx ON review_comments(session_id, created_at);

CREATE TABLE checkpoints (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  commit_sha TEXT NOT NULL,
  turn INTEGER NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX checkpoints_session_idx ON checkpoints(session_id, created_at DESC);

-- +goose Down
DROP TABLE checkpoints;
DROP TABLE review_comments;
ALTER TABLE approvals DROP COLUMN plan;
ALTER TABLE sessions DROP COLUMN diff_del;
ALTER TABLE sessions DROP COLUMN diff_add;
ALTER TABLE sessions DROP COLUMN base_ref;
ALTER TABLE sessions DROP COLUMN branch;
ALTER TABLE workspaces DROP COLUMN auto_checkpoint;
ALTER TABLE workspaces DROP COLUMN base_branch;
