-- +goose Up
-- Loops: a template whose report field says "not done yet" iterates on the
-- same session until the field turns truthy or the iteration budget runs out.
-- Every iteration is an ordinary run row, so a loop is a chain of runs that
-- share one session (and therefore one context and one cost).
CREATE TABLE loops (
  id TEXT PRIMARY KEY,
  template_id TEXT NOT NULL REFERENCES templates(id),
  session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
  origin TEXT NOT NULL,
  origin_ref TEXT NOT NULL DEFAULT '',
  until_field TEXT NOT NULL DEFAULT 'done',
  max_iterations INTEGER NOT NULL DEFAULT 5,
  iteration INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL CHECK (state IN ('running','done','exhausted','failed','stopped')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX loops_state_idx ON loops(state, created_at DESC);

-- '' = no loop; else the report field whose truthiness ends the loop.
ALTER TABLE templates ADD COLUMN loop_until TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN loop_max INTEGER NOT NULL DEFAULT 0;

ALTER TABLE runs ADD COLUMN loop_id TEXT REFERENCES loops(id);
ALTER TABLE runs ADD COLUMN iteration INTEGER NOT NULL DEFAULT 0;
CREATE INDEX runs_loop_idx ON runs(loop_id, iteration);

-- +goose Down
DROP INDEX runs_loop_idx;
ALTER TABLE runs DROP COLUMN iteration;
ALTER TABLE runs DROP COLUMN loop_id;
ALTER TABLE templates DROP COLUMN loop_max;
ALTER TABLE templates DROP COLUMN loop_until;
DROP INDEX loops_state_idx;
DROP TABLE loops;
