# Architecture

```
browser (React SPA) ── HTTPS ── styr (Go) ── stdio JSONL ── claude -p (one process per open session)
                                  ├── SQLite (own event log)    └── <data>/users/<id>/.claude transcripts
                                  ├── in-process bus → SSE
                                  └── harness adapter, sessions service, auth
```

One Go binary serves both the JSON/SSE API and the embedded React single-page app. There is no
separate frontend server in production; `web/embed.go` bakes the built SPA into the binary and
`internal/api/spa.go`'s handler serves it as the fallback route. SQLite is the only datastore —
no external database, cache or queue.

## Request and event flow

1. The browser talks to the Go API over HTTPS: `GET/POST/PUT /api/v1/...` for everything except
   the live feed, and `GET /api/v1/events` (Server-Sent Events) for that feed. `internal/api`
   (`router.go`, one `*_handlers.go` file per resource) is the only package that knows HTTP.
2. Handlers call into `internal/sessions`' `Service`, the sessions domain layer. Creating or
   sending to a session goes through `Service.Create`/`Service.Send`, which look up the owning
   user's (or the service) Claude token, resolve the profile and workspace, and hand off to the
   harness.
3. `internal/harness/claude` is the one harness implementation in v0.1: it starts
   `claude -p --input-format stream-json --output-format stream-json ...` as a child process per
   open session (`internal/harness/claude/process.go`), with that user's own `HOME` set to
   `<data_dir>/users/<user-id>` so Claude Code's own state, credentials and settings never cross
   between users. `internal/harness/claude/harness.go` builds the CLI's argv from a `StartSpec`;
   `codec.go` decodes each stdout line into a `harness.Event` and encodes replies
   (`EncodeUser`, `EncodeDecision`, `EncodeInterrupt`) back onto stdin. The `Harness` interface
   (`internal/harness/types.go`) is adapter-shaped on purpose — a second harness (e.g. Codex CLI,
   planned for v1.0) implements the same interface without touching `internal/sessions`.
4. `internal/sessions/runner.go`'s `pump` goroutine (one per open session) drains the harness
   process's event channel for the session's lifetime. For every event it: persists it to
   SQLite (`internal/db/events.go`, an ordered per-session log), publishes it on the in-process
   bus (`internal/events/bus.go`), and applies state side effects (model, cost, token counters,
   session state transitions) via the sessions repo.
5. The bus fans each published `events.Message` out to every subscriber; `internal/api`'s SSE
   handler is one such subscriber per connected browser tab, filtering by the visibility rule
   below before writing an `event:`/`data:` line to the response. This is how a tool call
   streams from the child process to every open browser tab watching that session in
   near-real-time, without polling.
6. A REST write (e.g. approving a prompt) goes the other way: handler → `sessions.Service` →
   harness `Process`, which encodes a reply and writes it to the child's stdin.

## API versioning

`docs/openapi.yaml` is the source of truth for `/api/v1`, not a description generated from it:
every handler in `internal/api` is written by hand against the document, not the other way
round. Its `info.version` (`1.0.0` as of card T62's API freeze) is the API's own version,
independent of `VERSION` (the styr binary's release number) — the two move at different rates,
which `GET /api/v1/version` reports separately as `api_version` and `version`. Within the `1.x`
line the API only changes additively (new operations, new optional fields, new enum values; see
docs/API.md's compatibility promise for the full rule and how deprecations are announced), so a
client generated against `1.0.0` keeps working, unmodified, against every later `1.x` release —
the `/api/v1` path prefix itself only bumps at a genuine major version. `hack/openapi-compat`
(wired into CI as `make api-compat`) enforces this mechanically: it diffs the working tree's
`docs/openapi.yaml` against the previous release tag's copy and fails the build on a removed
path, a removed or retyped schema property, or a newly required request-body field — the
categories of change a hand-written client would actually break on.

## The permission round trip

Claude Code is started with `--permission-prompt-tool stdio`, which is the same mechanism the
Agent SDK uses when a `canUseTool` callback is set. When the running `claude` process wants to
call a tool it is not already allowed to use unconditionally, it writes a `control_request`
(`subtype: can_use_tool`) line to its stdout instead of proceeding. `codec.go` decodes that into
a `harness.Event`; `runner.go` turns it into a persisted `Approval` row and a
`approval.created` bus message, so it shows up in every eligible user's inbox immediately.
`internal/risk` classifies the requested tool and input into a risk tier (read / write / exec /
destructive) shown alongside the prompt.

The child process blocks on that one tool call until it receives an answer. A decision made in
the UI (allow, deny, or edit-then-allow) becomes a `sessions.Service.Decide` call, which calls
`harness.EncodeDecision` to build a `control_response` line and writes it to the child's stdin;
the child then either runs the tool or reports it declined, and the transcript continues.
Nothing in this path uses `--dangerously-skip-permissions` or `--permission-mode
bypassPermissions` — Styr only ever answers prompts the CLI itself raises, never removes them.
An unattended session (service token, no interactive owner) has the same round trip, but a
request left unanswered past `approval_timeout` defaults to deny rather than blocking forever.

## Triggers, runs and unattended sessions (v0.2)

A trigger turns an inbound webhook into an unattended session without any of the request/event
flow above changing shape — it just supplies the "browser" side programmatically:

1. `POST /hooks/{slug}` reaches `internal/triggers.Service.Deliver` (outside `/api/v1`, no
   cookie/CSRF, authenticated only by the trigger's own bearer secret). It authenticates,
   normalises the payload (`internal/templates.Normalize`), renders the dedupe key, applies
   cooldown/dedupe/storm-cap gating, and — if accepted — writes a `deliveries` row and calls
   `runs.Engine.Start`.
2. `runs.Engine.Start` renders the template's title and prompt (`internal/templates.Render`) and
   calls `sessions.Service.Create` exactly as an interactive `POST /sessions` would, except
   `Owner: nil` (the service token), `Origin: webhook`, and the template's `JSONSchema` and
   `SystemPrompt` passed through to `StartSpec` (ADR-009). It generates the run id first and
   hands it to the session as `RunID`/`OriginRef`, so the session always points back at the run
   that owns it, then inserts a `runs` row with outcome `running`.
3. From here the session runs exactly like any other: `sessions/runner.go`'s `pump` goroutine
   persists and publishes its events, and the same permission round trip applies (an unattended
   session's approval just has no one specific owner to page — see Visibility rules below).
4. **The run engine follows the run to completion over the same bus every SSE connection reads
   from.** `Engine.Run` subscribes once (`bus.Subscribe`, a 512-message buffer) and reacts to
   three message kinds for whichever session a `runs` row names: `session.event` with a `result`
   payload closes the run out `success`/`failed` and extracts the structured report;
   `session.state` turning `failed` closes it `failed`; `approval.created` fires a one-time
   `run.needs_human` notification without changing the run's outcome (see `docs/TRIGGERS.md`). A
   one-minute ticker in the same loop sweeps runs still `running` past the fixed 30-minute
   timeout, interrupting and closing their sessions and recording `timeout`. Because
   `sessions.Service.Create` can finish a fast session's first turn before `Start` has even
   inserted the `runs` row, `Start` also reads the session's just-persisted transcript once
   (`reconcile`) to catch a result that arrived before the row existed — the stored event log is
   always the authority; the bus and the reconciliation read are two paths to the same
   at-most-once close (`internal/runs/loop.go`'s `onceSet`).
5. Outcomes and approvals alike can produce an outbound notification (`internal/notify`); a run
   itself keeps no notification state beyond the once-per-run bookkeeping above.

`internal/templates` (rendering, normalisation, the seeded Grafana template) and `internal/notify`
(ntfy/webhook senders) have no data-layer dependency of their own — both take plain inputs and are
composed by `internal/triggers` and `internal/runs` respectively, which do own the repositories.

## Visibility rules

- A session belongs to a user (`owner_user_id`) or, for unattended runs, to nobody
  (`owner_user_id` is null).
- Members see only their own sessions.
- Admins see every session.
- Unattended sessions (`owner_user_id` null) are visible to every member, not just admins —
  they're shared infrastructure, not anyone's private conversation.
- `internal/sessions/service.go`'s `getVisible` enforces this on every read and write path, not
  just listing, so it can't be bypassed by session ID.
- A session cannot start unless its owner (or, for unattended sessions, the service token) has a
  verified Claude token.

## Switching a session's model or effort (v0.2)

The CLI only takes `--model`/`--effort` as start-up flags — there is no way to change them on a
live process. `sessions.Service.SwitchModel` (`internal/sessions/model.go`) therefore switches by
restarting: it refuses while the session is `waiting` on an approval (a live `control_request`
only the current process can answer), otherwise closes the tracked process, persists the new
`model`/`effort` on the session row, and starts a resumed process (`--resume <same session id>`)
with the new flags and no first message — the CLI keeps the transcript under that session id, so
nothing in Styr's own event log or the CLI's own history is lost, and the next operator turn goes
to the new process. The state change publishes a `session.state` message on the bus like any
other transition, which is how the session header's "Resuming with `<model>`…" indicator and
every open tab learn about it. The session header's model/effort controls (`POST
/sessions/{id}/model`) are the only way to change them mid-session; the CLI's own `/model` and
`/effort` slash commands are deliberately hidden from the composer menu for exactly this reason
(next section).

## The composer's slash-command menu (v0.2)

Every `system`/`init` message the CLI sends carries a flat `slash_commands` array — custom
project/user commands, plugin skills, and the CLI's own built-ins, all mixed together with no
field saying which is which (`internal/harness/claude/testdata/PROTOCOL.md`, "Init:
`slash_commands`"). `harness.Init` decodes it into `Init.SlashCommands`;
`sessions/runner.go`'s `pump` stores it on the session row on every init (including after a
model-switch resume), so the composer's `/` menu can offer it without a live process to ask.

Not every built-in survives headless (`-p`) mode, though, so `internal/harness/claude/builtins.go`
classifies the ones Styr has verified against the real CLI
(`testdata/PROTOCOL.md`'s "Built-in slash commands in `-p` mode" spike) and `GET /status` exposes
the hidden set as `hidden_commands`:

- `compact` and `cost` run over the pipe exactly as typed and are offered normally.
- `clear` is hidden: it resets the CLI's own conversation and re-inits under a **new** session
  id, silently orphaning the id Styr resumes with — there is no way for the UI to follow that.
- `doctor`, `color`, `reload-plugins` are hidden: the CLI's own init message reports them as
  terminal-only via `terminal_slash_commands`.
- `model` and `effort` are hidden: Styr owns them through the session header's selects (previous
  section), which persist the choice and restart the process; letting the CLI change them behind
  Styr's back would leave the stored values wrong.

`web/src/components/session/slashCommands.ts`'s `buildSlashCommands` merges Styr's own actions
(`/model`, `/effort`, `/new`, `/interrupt`, `/close`, `/help`) with the session's `slash_commands`
minus that hidden set (and minus anything Styr already owns by name); selecting a CLI-owned entry
inserts and sends it as an ordinary user turn — the CLI executes custom commands and skills in
`-p` mode the same way it executes `/compact`.

## Worktrees and the review flow (v0.3)

A worktree-enabled workspace (`Workspace.Worktrees`) gives every session its own git worktree
instead of running in the workspace's shared checkout, which is what makes a real per-session
diff, checkpoint and commit possible without one session's edits colliding with another's or
with a human's own working copy. `internal/gitops` is the only package that shells out to `git`
and `gh` (argv-based `os/exec`, no shell, a whitelisted environment, a 60s timeout per call); it
knows nothing about sessions or HTTP, and `internal/sessions` is its only caller.

1. **Session create → `gitops.AddWorktree` → CLI cwd.** `Service.Create` inserts the session row
   first, then — before starting the harness process at all — calls `s.createWorktree`
   (`internal/sessions/worktree.go`), which resolves the workspace's base branch (its own
   `base_branch`, or the checkout's current branch when that's empty), creates the worktree at
   `<workspace path>/.styr/worktrees/<session id>` on a new `styr/<short id>-<slug>` branch via
   `gitops.Repo.AddWorktree`, and records the resulting path, branch and starting commit
   (`base_ref`) on the session row. `sessionCwd` then makes that path the harness process's
   `cwd`, so the CLI's very first tool call already runs inside the worktree, never the shared
   checkout.
2. **Result → checkpoint + diff stats + `session.stats`.** `runner.go`'s `pump` goroutine calls
   `s.afterResult` after every `result` event. For a worktree session with `auto_checkpoint` on,
   that commits whatever the turn changed under a fixed `Styr <styr@local>` identity
   (`gitops.Worktree.Checkpoint`, a no-op commit when the turn touched nothing) and records a
   `checkpoints` row when it actually committed; either way it recomputes the diff against
   `base_ref` (`gitops.Worktree.DiffSummary`) and persists the new add/del counts onto the
   session row. `publishStats` then puts those same counts on the bus as part of the
   `session.stats` message every open tab already listens for (turns/cost/tokens), which is how
   the sessions list's `+42 −18` diff badge and the Review tab's file list stay live without a
   dedicated event kind.
3. **Review endpoints → gitops.** Every `/sessions/{id}/{diff,diff/file,comments,review,commit,
   pr,checkpoints,rewind,discard,patch}` handler (`internal/api/review_handlers.go`,
   `review_actions_handlers.go`) goes through `sessions.Service`'s review methods
   (`review.go`, `review_actions.go`), which resolve the session's `gitops.Worktree` handle and
   call straight into it — there is no intermediate cache or projection of git state in SQLite
   beyond the `checkpoints` table and the session's own `diff_add`/`diff_del`/`branch`/`base_ref`
   columns. A session with no worktree answers every one of these 422 (`sessions.ErrNoWorktree`).
   Full behaviour (branch naming, checkpoint/rewind semantics, commit/PR requirements, plan
   approval) is `docs/REVIEW.md`.

## Schedules, loop advancement and stats (v0.4)

v0.4 adds two more ways to start a run without a human clicking anything, and a pair of
read-only views over what the fleet is doing. Full behaviour (cron syntax, the tick, loop
semantics, Gantt derivation, cost windows) is `docs/SCHEDULES.md`; this section is only the
architectural shape.

- **The scheduler loop.** `internal/schedules.Service.Run` is a third long-lived goroutine
  started in `cmd/styr/serve.go` alongside `bg.Runs.Run` (the run-timeout sweep) — `go
  bg.Schedules.Run(maintCtx)`, sharing that same maintenance context so both stop together at
  shutdown. It ticks a plain `time.Ticker` every 30s and, per tick, loads every enabled schedule
  due by `next_run_at`, fires each through the same `runs.Engine.Start` a webhook trigger calls
  (`RunStarter`/`RunLookup` are narrow interfaces `*runs.Engine` satisfies, so
  `internal/schedules` depends on the run engine's shape, not its concrete type), and records a
  `schedule_firings` row plus the schedule's next `next_run_at` regardless of outcome. It never
  touches HTTP or the harness directly — starting a run, following it to completion, and timing
  it out all still go through `internal/runs` exactly as they do for a webhook-started run.
- **Loop advancement in the run engine.** A loop is not a separate execution path either: it is
  `internal/runs/loops.go`, a file inside the run engine package that hooks the same place every
  run already closes out. Whenever a run finishes with a `LoopID` set, `Engine.advance` runs: it
  loads the loop, decides done/exhausted/failed/keep-going from the report and the iteration
  count, and — to keep going — renders the template again and calls `sessions.Send` on the loop's
  existing session id rather than `sessions.Create`, so an iteration is a resumed turn on a live
  session, not a new session. The variables a loop's first iteration started with live only in an
  in-process map (`varsCache`) keyed by loop id — there is no table column for them — which is
  what makes a loop's variables a memory-only, restart-losing concern (`docs/SCHEDULES.md`, "Loop
  variables are held in memory only").
- **`internal/stats` is read-only.** Both its exports, `Service.Gantt` and `Service.Costs`,
  compute their answer fresh from existing tables (`sessions`, `events`, `runs`) on every
  request — there is no stats table, no background aggregation job, no cache invalidated by
  writes elsewhere. That is a deliberate simplicity choice at "a handful of users, one process":
  the package's own doc comment states it never writes to the database, and every query in it is
  a plain `SELECT`. It depends on `internal/db` (the shared `*db.DB` for ad-hoc SQL plus the
  `Sessions`/`Users` repos for visibility-aware listing and owner names) and
  `internal/sessions.Actor` for the visibility rule, but nothing downstream depends on it — the
  API layer is `stats`'s only caller.
- **The shutdown sequence cancels the request base context before HTTP shutdown.** `runServe`
  builds `reqCtx` (`context.WithCancel(context.Background())`) and wires it as the HTTP server's
  `BaseContext`, so every request's `context.Context` — including a long-lived `GET
  /api/v1/events` SSE stream — derives from it rather than from the per-request context
  `net/http` would otherwise hand out on its own. On SIGINT/SIGTERM, `reqCancel()` is called
  **first**, before `srv.Shutdown(shutCtx)`: cancelling `reqCtx` ends every open SSE stream (and
  any handler blocked on it) immediately, so `http.Server.Shutdown` — which only waits for
  in-flight handlers to return, closing idle connections itself — has handlers that actually
  finish inside its own `shutdownTimeout` (15s) instead of blocking on a stream that would
  otherwise run until the timeout forcibly killed the connection. `sessionsSvc.Shutdown` (closing
  every open harness process cleanly) runs afterwards, under its own 15s timeout. Ordering
  matters here specifically because of the scheduler and run-engine goroutines added in v0.4: both
  already stop via `maintCtx` (cancelled by its own `defer maintCancel()`), independently of the
  HTTP shutdown sequence, but a schedule's in-flight "Run now" HTTP request or an SSE tab watching
  a loop's session both rely on `reqCancel()` running before `srv.Shutdown` to unblock promptly.

## Pipelines: advancing a DAG on run events (v0.5)

A pipeline is not a separate execution path from a run any more than a schedule or a loop is: a
pipeline run's step is an ordinary `internal/runs.Engine.Start` call, and the pipeline executor's
whole job is deciding, from the run engine's own bus events, which step to start next. Full
behaviour (YAML format, validation, fan-out, retries, worktree modes) is `docs/PIPELINES.md`;
this section is only the architectural shape.

- **The run engine gained two bus events for this.** `internal/runs/loop.go`'s `finish` — the one
  place every run closes out, whatever outcome ends it — now calls `publishFinished`, which
  publishes `run.finished` (success) or `run.failed` (every other outcome) on the bus, carrying
  `{run_id, step_run_id, outcome}`. `step_run_id` is `nil` for a run outside a pipeline (an
  ordinary trigger/schedule/loop/UI-started run); a pipeline step's run carries the
  `internal/pipelines` step-run id it belongs to (`runs.RunInput.StepRunID`, plumbed onto the run
  row by migration `00008_pipelines.sql`'s `runs.step_run_id` column). It is published **after**
  the run row is written, so a subscriber that reads the run back over the API always sees the
  already-finished row — the same ordering guarantee `session.event`/`session.state` already give
  every other bus consumer.
- **`internal/pipelines.Executor.Run` subscribes to that bus** (`busBuffer` 512, the same
  generous depth `runs.Engine.Run`'s own subscription uses) alongside a 30s ticker
  (`sweepInterval`), structurally identical to `runs.Engine.Run`'s own select loop — started as a
  third long-lived goroutine in `cmd/styr/serve.go`, alongside `bg.Runs.Run` and
  `bg.Schedules.Run`, sharing the same `maintCtx`. `handle` ignores every bus message except
  `run.finished`/`run.failed` carrying a non-nil `step_run_id`, and for those calls
  `completeStep`, which loads the step run, records success (the report) or failure, and —
  within the step's retry budget — either opens a new attempt or lets the failure cascade.
- **The advance loop.** `completeStep` and `Start` both end by calling `advanceLocked`
  (`internal/pipelines/advance.go`), which re-reads every step-run of the pipeline run, expands
  any fan-out node whose dependencies just succeeded (`foreach` rendered against the now-available
  `steps.<id>.report`), starts every pending step-run whose node is now eligible (rendering
  `with`, resolving the template, calling `runs.Engine.Start` with `Origin: pipeline` and the new
  step-run's id), and settles the pipeline run out (`success`/`failed`) once nothing is left
  pending or running. A `sync.Mutex` (`Executor.mu`) serialises every graph mutation: it is held
  across the whole of `advanceLocked`, including the `runs.Engine.Start` calls inside it, so a bus
  event arriving mid-`advance` (a fast step finishing while a slower sibling in the same level is
  still being started) can never see and act on a half-updated set of step-runs.
- **The timeout sweep doubles as bus-message reconciliation.** `Executor.Tick`, called every 30s
  by the same goroutine that subscribes to the bus, closes out any pipeline run older than its
  definition's `timeout` (default 2h, capped at 24h) exactly like `Cancel` does — but for a run
  still within its timeout, it also re-reads every `running` step-run's underlying `runs` row
  directly and applies its outcome if it has already finished (`reconcile`). This is the same
  problem `runs.Engine`'s own reconciliation solves for a session that finishes before its run row
  exists (see "Triggers, runs and unattended sessions" above): a bus message can be dropped (a
  full subscriber channel, a server restart between the run finishing and the message being
  handled), and without this sweep a pipeline could get stuck `running` forever waiting for a
  message that already came and went.
- **Sessions gained a way to join an existing worktree.** A `worktree: shared` step continues in
  a dependency's own worktree rather than creating a fresh one:
  `sessions.CreateInput.WorktreePath` (migration `00009_worktree_share.sql`'s
  `sessions.worktree_shared` column, informational only — every git operation still addresses the
  worktree by its existing path column) routes `sessions.Service.Create` to `attachWorktree`
  instead of the ordinary `createWorktree`, which resolves the path's branch from git and its base
  commit from the session that originally created it, rather than creating a new `git worktree`
  at all. Nothing removes a step's worktree when the pipeline finishes; it survives for review
  exactly like any other worktree session's, addressed via that step's own session page.
- **Trigger and schedule targets became nullable, on a single migration connection.** A trigger
  or schedule now points at either a template or a pipeline (`template_id`/`pipeline_id`, exactly
  one). Migration `00008_pipelines.sql` added the `pipeline_id` columns, but `template_id` was
  still `NOT NULL` from earlier migrations, leaving no legal value for a row that targets a
  pipeline. Migration `00010_pipeline_targets.sql` relaxes both `triggers.template_id` and
  `schedules.template_id` to nullable — SQLite cannot drop a `NOT NULL` constraint in place, so
  each table is rebuilt by SQLite's documented procedure (`PRAGMA foreign_keys = OFF`, create a
  `_new` table with the relaxed schema, copy every row, drop the old table, rename the new one
  into place, `PRAGMA foreign_keys = ON`). `PRAGMA foreign_keys` is a per-connection setting and a
  no-op inside a transaction, which is why `internal/db.migrate` opens goose's migration runner on
  a pool capped at **one** connection (`migrator.SetMaxOpenConns(1)`, closed again before `Open`
  hands back the application's own pool): a second pooled connection wouldn't share the pragma
  toggle, and the statements have to run outside a transaction (`-- +goose NO TRANSACTION`) for
  the pragma to take effect at all.

## What lives where

```
cmd/styr/            subcommand dispatch (serve|migrate|doctor|version), composition root (wire.go)
internal/config/     Config struct, Load/Validate, STYR_* env overrides
internal/harness/    Harness adapter interface + the Claude Code implementation (claude/) and a
                     scripted fake (fake/) used by tests and dev
internal/events/     in-process pub/sub bus behind the SSE endpoint
internal/domain/     core types: Session, Event, Approval, Workspace, Profile, User, Template,
                     Trigger, Delivery, Run, Schedule, ScheduleFiring, Loop, Pipeline,
                     PipelineRun, StepRun, NotificationChannel, APIToken, errors
internal/db/         SQLite open + goose migrations + one repo file per table
internal/crypto/     AES-GCM seal/open for tokens at rest
internal/risk/       tool+input → risk tier classification
internal/gitops/     git/gh worktrees, diffs, checkpoints, commit, push and PR — the only
                     package that shells out to git or gh
internal/sessions/   Service (create/send/decide/interrupt/close/list/get/switch-model), the
                     per-session runner goroutine, the open-session slot scheduler and idle
                     reaper, worktree lifecycle and review operations (worktree.go, review.go,
                     review_actions.go)
internal/templates/  Go text/template rendering of prompts/titles/dedupe keys from a trigger
                     payload, per-kind normalisation (generic/grafana/github), the seeded
                     Grafana template
internal/triggers/   templates and triggers CRUD, the `/hooks/{slug}` pipeline: auth, normalise,
                     dedupe/cooldown/storm-cap, delivery log, replay, test
internal/runs/       the unattended run engine: starts a session from a rendered template, follows
                     it to an outcome over the event bus, times out a stale run, advances loops
                     (loops.go) on report arrival
internal/schedules/  cron table, next-run computation (robfig/cron), the 30s scheduler tick that
                     fires due schedules through internal/runs, firings log, cron preview/describe
internal/pipelines/  YAML DAG parser and validator (def.go), the executor that advances a
                     pipeline run on internal/runs' bus events and a 30s timeout/reconcile sweep
                     (executor.go, advance.go), the pipeline-run read model (view.go)
internal/stats/      read-only fleet views: the Gantt's running/waiting/idle segments derived
                     from events, and cost aggregation by day/owner/origin/template
internal/notify/     outbound ntfy and generic-webhook senders for run events
internal/auth/       OIDC provider setup, PKCE login/callback, session cookies, personal API
                     token bearer auth, auth middleware
internal/api/        HTTP router, one *_handlers.go per resource, SPA fallback handler
web/                 Vite + React SPA (source of truth for the UI), embedded into the binary
deploy/              install.sh, systemd unit, Dockerfile, compose.yaml, config.example.yaml
docs/                this file, specs, ADRs, OpenAPI contract, task-card build plan
testdata/            shell fake of the `claude` CLI, replaying a fixture, used by e2e and
                     `make dev-backend`
```
