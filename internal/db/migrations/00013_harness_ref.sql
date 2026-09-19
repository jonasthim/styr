-- +goose Up
-- T65: keep a harness-native conversation id on the session row.
--
-- A Styr session id is Styr's own; some CLIs resume by it (Claude Code takes
-- `--resume <session id>`), but the Codex CLI resumes by *its* thread id,
-- which only exists once a turn has run and the CLI has reported it
-- (`thread.started`). Until now that id lived in memory on the process
-- object, so reopening a closed session — or switching model, which restarts
-- the process — started a fresh Codex thread and lost the CLI-side context
-- (docs/HARNESSES.md, "One process per turn").
--
-- harness_ref stores whatever the running harness reported as its own id on
-- the init message (harness.Init.HarnessRef), so a resumed process can hand
-- it back as harness.StartSpec.ResumeRef. It is empty for a session that has
-- never started a process, and stays empty for a harness that has no id of
-- its own to resume by — the Claude harness leaves it unset and keeps
-- resuming by the Styr session id.
ALTER TABLE sessions ADD COLUMN harness_ref TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN harness_ref;
