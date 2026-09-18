-- +goose Up
-- Worktree reuse: a session can now be created ON an existing worktree
-- instead of a fresh one (sessions.CreateInput.WorktreePath), so a pipeline
-- step can continue in the worktree the previous step left off in.
-- worktree_shared is informational only — every review and Discard
-- operation identifies a shared worktree by its path column, already
-- present, not by this flag.
ALTER TABLE sessions ADD COLUMN worktree_shared INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN worktree_shared;
