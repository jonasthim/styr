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

## What lives where

```
cmd/styr/            subcommand dispatch (serve|migrate|doctor|version), composition root (wire.go)
internal/config/     Config struct, Load/Validate, STYR_* env overrides
internal/harness/    Harness adapter interface + the Claude Code implementation (claude/) and a
                     scripted fake (fake/) used by tests and dev
internal/events/     in-process pub/sub bus behind the SSE endpoint
internal/domain/     core types: Session, Event, Approval, Workspace, Profile, User, errors
internal/db/         SQLite open + goose migrations + one repo file per table
internal/crypto/     AES-GCM seal/open for tokens at rest
internal/risk/       tool+input → risk tier classification
internal/sessions/   Service (create/send/decide/interrupt/close/list/get), the per-session
                     runner goroutine, the open-session slot scheduler and idle reaper
internal/auth/       OIDC provider setup, PKCE login/callback, session cookies, auth middleware
internal/api/        HTTP router, one *_handlers.go per resource, SPA fallback handler
web/                 Vite + React SPA (source of truth for the UI), embedded into the binary
deploy/              install.sh, systemd unit, Dockerfile, compose.yaml, config.example.yaml
docs/                this file, specs, ADRs, OpenAPI contract, task-card build plan
testdata/            shell fake of the `claude` CLI, replaying a fixture, used by e2e and
                     `make dev-backend`
```
