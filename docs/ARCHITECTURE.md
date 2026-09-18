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

## What lives where

```
cmd/styr/            subcommand dispatch (serve|migrate|doctor|version), composition root (wire.go)
internal/config/     Config struct, Load/Validate, STYR_* env overrides
internal/harness/    Harness adapter interface + the Claude Code implementation (claude/) and a
                     scripted fake (fake/) used by tests and dev
internal/events/     in-process pub/sub bus behind the SSE endpoint
internal/domain/     core types: Session, Event, Approval, Workspace, Profile, User, Template,
                     Trigger, Delivery, Run, NotificationChannel, APIToken, errors
internal/db/         SQLite open + goose migrations + one repo file per table
internal/crypto/     AES-GCM seal/open for tokens at rest
internal/risk/       tool+input → risk tier classification
internal/sessions/   Service (create/send/decide/interrupt/close/list/get/switch-model), the
                     per-session runner goroutine, the open-session slot scheduler and idle reaper
internal/templates/  Go text/template rendering of prompts/titles/dedupe keys from a trigger
                     payload, per-kind normalisation (generic/grafana/github), the seeded
                     Grafana template
internal/triggers/   templates and triggers CRUD, the `/hooks/{slug}` pipeline: auth, normalise,
                     dedupe/cooldown/storm-cap, delivery log, replay, test
internal/runs/       the unattended run engine: starts a session from a rendered template, follows
                     it to an outcome over the event bus, times out a stale run
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
