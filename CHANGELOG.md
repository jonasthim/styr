# Changelog

All notable changes to Styr are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Version numbers are
`MAJOR.MINOR.PATCH`; see [docs/API.md](docs/API.md) for what "stable" means for the API
starting at `1.0.0`.

## [Unreleased]

## [1.0.3] - 2026-09-19

### Fixed

- iOS Safari no longer zooms the page when a text field is focused: on touch devices every
  input, textarea and select renders at 16px, the threshold below which Safari zooms in.

## [1.0.2] - 2026-09-19

### Fixed

- The session view on a phone: the header took a third of the screen (chips, full-width model
  and effort selects, token counts, the copy-id button) and the Activity/Review/Info panel a
  permanently open block above the composer, leaving the transcript a sliver a few lines tall.
  Below 900px the header is now a back link, the title, the state and Close on one row and the
  two selects on a second; the review buttons scroll sideways in one strip; the panel starts
  collapsed to its tab strip and opens as a sheet when a tab is tapped (tap again or the chevron
  to collapse); the composer drops the keyboard hints and gets a 44px Send. Desktop is
  unchanged apart from the model and effort selects, which were accidentally full width.
- `styr doctor` no longer doubles the status word in its skip and warn lines
  (`skip codex binary found (optional): codex not found ...`, not `...: skip: codex not found`).

## [1.0.1] - 2026-09-19

### Fixed

- `styr backup`, `styr restore` and `styr migrate` now load `/etc/styr/env` (or `STYR_ENV_FILE`)
  before reading the configuration, the way `styr doctor` already did, so a hand-run
  `sudo -u styr ... styr backup` on an installed host no longer fails with
  `secret_key must be at least 32 bytes`. `styr serve` does the same, which is a no-op under
  systemd. All of them print the same `note loaded N variable(s)` / `warn ... not readable`
  lines as `doctor`.

### Added

- `styr doctor` reports the Codex CLI as its own check, `codex binary found (optional)`: `skip`
  when the binary is not installed (Codex sessions are simply unavailable), `FAIL` only when it
  is present but cannot answer `--version`. Previously an absent Codex binary produced no line at
  all.

## [1.0.0] - 2026-09-19

"Boring and durable": a second harness, a stable and versioned API, an upgrade and backup
story, and roles for a handful of users.

### Added

- Codex CLI harness alongside Claude Code, behind the same internal harness interface: a Codex
  session can investigate read-only or edit inside a worktree, the same way a Claude Code
  session does. Codex approvals are not supported the same way Claude Code's are — Codex
  enforces its sandbox policy up front rather than asking per command — so a Codex session's
  inbox affordances differ from a Claude Code session's.
- A harness registry: `POST /api/v1/sessions` accepts a `harness`, falling back to the
  session's profile default and then to Claude Code; `GET /api/v1/status` reports every known
  harness with whether its binary actually answered at startup, so the new-session dialog can
  explain an unavailable choice instead of just hiding it.
- Personal Codex keys (Profile → Codex key, `PUT /api/v1/me/codex-key`): an OpenAI API key
  sealed at rest the same way a Claude token is, verified before it is stored, plus a separate
  service-wide Codex key for unattended runs.
- `styr backup <file>` and `styr restore <file>`: an online SQLite backup (`VACUUM INTO`, no
  downtime) plus a manifest recording the styr and schema version it was taken with. Restore
  refuses to run against a server that is still up, and refuses a backup whose schema is newer
  than the binary doing the restoring supports.
- `doctor` now checks free disk space and flags orphaned session worktrees left behind by a
  crash; `install.sh --version <v>` refuses to downgrade past the version already installed.
- CI now also greps for Codex's own dangerous bypass flag, alongside the existing Claude Code
  guard.
- The API is frozen at `docs/openapi.yaml`'s `info.version: 1.0.0`, every operation has a
  stable `operationId`, and `GET /api/v1/version` reports the running styr version, API
  version and build commit. `docs/API.md` is the new human-readable overview: authentication,
  the error envelope, pagination, the SSE event kinds, the inbound webhook, a curl walkthrough,
  and the compatibility promise below.
- A CI compatibility guard (`hack/openapi-compat`, `make api-compat`) diffs `docs/openapi.yaml`
  against the previous release tag's copy and fails the build on a removed operation or
  response field, a retyped field, or a new required request field.
- A `viewer` role: the same visibility as `member`, but every state-changing request is
  refused with `403 read_only` except a caller's own profile, personal API tokens and Claude
  token. The first user to ever sign in still always becomes admin, and an admin can never
  demote themselves or the last remaining admin.
- Per-workspace access lists for shared workspaces: `everyone` (default) or `listed`, admin-set
  (`PUT /api/v1/workspaces/{id}/access`). A workspace's `listed` allowlist never blocks an
  unattended start (webhook, schedule or pipeline step), only a by-hand one from the UI or API.

Within the `1.x` line the API only changes additively — new operations, new optional fields,
new enum values — never a removed or retyped response field or a new required request field;
see [docs/API.md](docs/API.md#compatibility-promise) for the full promise and how CI enforces
it.

## [0.5.0] - 2026-09-19

"Pipelines."

### Added

- YAML DAG pipelines: a pipeline is a small chain of steps, each one a template run whose
  structured report feeds the steps that come after it.
- Fan-out: a step's `foreach` over a list becomes N parallel step-runs, one per item.
- Retries: a failed step gets a fresh attempt within its own budget before the failure cascades
  to the whole pipeline run.
- Shared worktrees: a step can continue in a dependency's own worktree instead of a fresh one,
  so a triage → fix → verify chain edits and tests the same checkout.
- A live graph on the pipeline run page, showing every step's state, attempt and report as it
  happens.
- Pipelines can now start from a trigger or a schedule, not just by hand, the same as a
  template.
- `docs/PIPELINES.md`: the full YAML format, execution semantics and worktree modes.

## [0.4.0] - 2026-09-19

"Schedules and loops."

### Added

- Cron schedules that start a template run on a cadence, with overlap protection: a schedule
  never runs two of its own instances concurrently, it skips (and logs) the tick instead.
- Until-done loops: a template repeats its run on the same session until its structured report
  says it's done, or its iteration budget runs out.
- A fleet Gantt: one lane per active session, showing running, waiting and idle time over the
  last 1h, 6h or 24h.
- A cost dashboard: what the whole box has spent, per day, per user and per origin, plus the
  top templates by spend.
- `docs/SCHEDULES.md`: the full schedule, loop, Gantt and cost guide.

## [0.3.0] - 2026-09-19

"Review."

### Added

- A worktree per session on a worktree-enabled workspace: sessions run on their own branch,
  never in the workspace's shared checkout.
- Diff review with inline comments that become the session's next prompt once the review is
  sent.
- Commit (folds a session's checkpoints into one commit) and Open PR (push plus `gh pr create`)
  straight from the session view.
- Checkpoints after every turn, with rewind to any of them — files only, the chat is kept.
- Plan approval: a plan-mode session's finished plan renders as a checklist you approve or send
  back with comments.
- `docs/REVIEW.md`: the full worktree, review, checkpoint and plan-approval guide.

### Fixed

- Error responses no longer repeat their own error-category prefix twice (an error used to
  read like "conflict: conflict: ...").

## [0.2.0] - 2026-09-18

"Triggers."

### Added

- Templates and inbound webhook triggers (generic, Grafana, GitHub kinds), with dedupe, a
  cooldown window and a storm cap so a flapping alert can't flood the box.
- A seeded Grafana alert-investigation template, ready to point a Grafana contact point at.
- Runs and reports: every unattended investigation, its structured report and its cost, on a
  Runs page any signed-in user can see.
- ntfy and generic-webhook outbound notifications when a run finishes, needs a human, or fails.
- Per-user managed workspaces, cloned from a git URL or created empty on demand, instead of
  only admin-registered server paths.
- Personal API tokens, for scripting against the API without a browser session.
- A per-session model and reasoning-effort switch, changeable mid-session.
- A slash-command menu in the composer, fed by the CLI's own commands and skills.
- `docs/TRIGGERS.md`: the full trigger, run and notification setup guide.

## [0.1.1 to 0.1.4] - 2026-09-18

Small fixes and polish between the first release and the v0.2.0 feature work, folded into one
entry here rather than four separate ones.

### Added

- An onboarding redirect straight into a new user's first workspace, a "sign out everywhere"
  action, and a theme option that follows the system setting.

### Fixed

- A user's own chat turns are now recorded in the session transcript (previously only the
  assistant's side was kept).
- Database writes take an immediate-mode transaction lock up front, instead of occasionally
  hitting a "database is busy" error under concurrent writers.
- A failed Claude token verification now explains why (expired, revoked, wrong scope, ...)
  instead of failing silently.
- The install script now installs `git` as a dependency and locates the `claude` binary
  correctly without a login shell; `doctor` now loads the same environment file the service
  does, so its checks match what's actually running.
- `/etc/styr/env` is now readable by the `styr` group (and `doctor` warns when it isn't); the
  release workflow now attaches `install.sh` to each GitHub release as its own downloadable
  asset, rather than only inside the source archive.

## [0.1.0] - 2026-09-18

"Cockpit." The first release.

### Added

- OIDC login (PKCE) with per-user profiles; the first user to ever sign in becomes admin.
- A sessions list and a session view with tool-call blocks and an activity timeline.
- An inbox with approvals: allow, deny, or edit-then-allow a pending permission prompt from
  your phone.
- Workspaces and profiles.
- A command palette (`Cmd+K`) and full keyboard shortcuts.
- An install script and a Docker image; zero telemetry.
