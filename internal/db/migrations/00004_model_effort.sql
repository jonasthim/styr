-- +goose Up
-- Per-profile model and effort defaults, and the per-session effort and slash-command list
-- (sessions.model already exists and is filled from the CLI's init message).
ALTER TABLE profiles ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN slash_commands TEXT NOT NULL DEFAULT '[]';

-- The unattended builtin profiles get a cheaper, fixed model so an alert investigation does
-- not silently run on whatever the CLI defaults to. `interactive` keeps the CLI default
-- (empty model and effort).
UPDATE profiles SET model = 'sonnet', effort = 'medium' WHERE id IN ('investigate', 'remediate');

-- +goose Down
UPDATE profiles SET model = '', effort = '' WHERE id IN ('investigate', 'remediate');
ALTER TABLE sessions DROP COLUMN slash_commands;
ALTER TABLE sessions DROP COLUMN effort;
ALTER TABLE profiles DROP COLUMN effort;
ALTER TABLE profiles DROP COLUMN model;
