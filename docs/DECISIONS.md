# Decisions

## ADR-001: Drive the Claude Code CLI, not the Agent SDK
Date: 2026-09-18. Status: accepted.

The Agent SDK quickstart forbids claude.ai (subscription) login for SDK-built agents. Styr must
run on users' Max plans, so it spawns `claude -p --input-format stream-json --output-format
stream-json` and speaks the documented protocol. Permission prompts are routed over stdio with
`--permission-prompt-tool stdio`. Consequence: Styr owns a small JSONL codec and re-records
fixtures when the CLI changes.

## ADR-002: Observed stream-json protocol, CLI 2.1.276
Date: 2026-09-18. Status: accepted.

Fixtures in `internal/harness/claude/testdata` are the contract; full detail in that
directory's `PROTOCOL.md`. Summary, one line per message type actually observed:

- `system`/`init`: top-level `session_id`, `model`, `tools[]`, `cwd`, `permissionMode` — not
  nested under `message`.
- `assistant`: `message.content[]` blocks, one block per top-level line in practice
  (`text`, `tool_use` with `input`, and an undocumented `thinking` block); `session_id` and
  `parent_tool_use_id` are top-level siblings of `message`.
- `user`: either the `--replay-user-messages` echo of our own turn (`message.content` is a
  plain string — decodes to no event) or a `tool_result` (`message.content[]` array with
  `tool_use_id`, `content` as a string, `is_error`).
- `stream_event`: `event.type` `content_block_delta` with `delta.type == "text_delta"` and
  `delta.text` carries the partial text; several other `event.type` values
  (`message_start`/`content_block_start`/`content_block_stop`/`message_delta`/`message_stop`)
  carry no text and can be ignored.
- `result`: top-level `subtype`, `is_error`, `num_turns`, `total_cost_usd`, `duration_ms`,
  `result` (final text), and `usage.input_tokens`/`usage.output_tokens` nested under `usage`.
- `control_request`/`control_response` (the `can_use_tool` permission handshake): **not
  observed** in three recorded sessions, including two attempts at the dedicated Bash-tool
  fixture (`--permission-mode default` then `--permission-mode manual`, per the task card's
  retry rule). The Bash tool ran with no prompt and `result.permission_denials: []` both times.
  Likely cause: the recorder spawns `claude` without isolating `HOME`, so it inherited the
  operator's own `~/.claude` settings, which apparently already allow-list the command run.
  `04_multi_turn.jsonl` and `05_interrupt.jsonl` were not recorded (stopped per the card's rule
  to leave the fallback decision — re-record with an isolated `HOME`, or accept the
  `control_request`/`control_response` shapes as specified from the Agent SDK's documented wire
  format without a fixture — to a human before spending more of the session budget).

Re-record when the CLI major or minor version changes, and re-attempt fixture 03 (and then 04,
05) once the permission-prompt question above is resolved.

## ADR-003: Go single binary
Date: 2026-09-18. Status: accepted.

Self-hosters want one artifact, not a stack. Context: the audience is homelab operators running
on modest hardware, often without a container runtime already installed. Decision: Styr is one
Go binary embedding the built React SPA (`web/embed.go`) and using SQLite, no external services.
Consequences: `styr serve` runs standalone; cross-compiling linux/amd64 and linux/arm64 is
trivial; the tradeoff is no horizontal scaling story, which the audience doesn't need.

## ADR-004: Tailwind and Radix over a component kit
Date: 2026-09-18. Status: accepted.

Context: "mission control" design direction needs a dense, distinctive UI, not a generic admin
template. Decision: Tailwind CSS for layout/spacing utilities plus unstyled Radix primitives
(Dialog, Tabs, DropdownMenu, Switch, Toast, Tooltip) for accessible behaviour, styling everything
ourselves against the design tokens. Consequences: more component code to own than a kit like
shadcn/MUI would need, but no fighting a kit's own visual opinions or bundle weight later.

## ADR-005: SSE over WebSockets
Date: 2026-09-18. Status: accepted.

Context: the browser needs a live one-way feed of session events and state changes; the browser
never needs to push a stream back the other way (writes are ordinary REST calls). Decision: one
`GET /api/v1/events` Server-Sent Events endpoint per browser tab, fed by an in-process bus.
Consequences: simpler server code (plain `http.ResponseWriter` flush, no handshake/framing
library, auto-reconnect built into `EventSource`) at the cost of one feed per tab rather than a
single duplex socket; acceptable at "a handful of users."

## ADR-006: Zero telemetry
Date: 2026-09-18. Status: accepted.

Context: self-hosters explicitly distrust default telemetry and login walls (see the spec's
landscape survey). Decision: Styr sends no analytics, crash reports or usage data anywhere, ever,
and has no login wall beyond the operator's own OIDC provider; any future update check is opt-in.
Consequences: no aggregate usage visibility for the maintainer, which is the deliberate trade for
trust with this audience.

## ADR-007: OIDC-only auth with per-user Claude tokens
Date: 2026-09-18. Status: accepted.

Context: Styr must run on users' own Max plans (ADR-001), and a shared multi-user deployment
needs real identity, not a single shared password. Decision: login is OIDC (PKCE) only, no local
password store; each user pastes their own `claude setup-token` token into their profile, so
their sessions bill their own plan and use their own `HOME`; an admin sets a separate service
token for unattended runs. Consequences: Styr owns no credential recovery flow (that's the IdP's
job) and needs an OIDC provider to be usable at all — acceptable for the homelab audience, who
already run one.

## ADR-008: CSS cascade layers
Date: 2026-09-18. Status: accepted.

Context: the base reset (box-sizing, margins, `color-scheme`, font stacks) and Tailwind's
generated utilities both apply to the same elements; without layering, source order or
specificity fights determine which one wins, unpredictably. Decision: the base reset lives in
`@layer base` (`web/src/styles/base.css`) so Tailwind's utility classes — unlayered — always win
regardless of source order. Consequences: any future hand-written CSS that should win over
utilities must go in a layer declared after `utilities`, or stay unlayered deliberately.

## ADR-009: Structured reports via --json-schema
Date: 2026-09-18. Status: accepted.

Context: v0.2 unattended runs (Grafana alert investigations and similar triggers) need a
machine-parseable report — severity, diagnosis, confidence — rather than free-form prose, so the
Runs page can render a consistent summary without an LLM-based extraction step. Decision: pass
the template's report schema to the CLI as `--json-schema` and its extra instructions as
`--append-system-prompt` (`harness.StartSpec.JSONSchema`/`SystemPrompt` → `claude.BuildArgs`);
the CLI constrains the model's final turn to a synthetic `StructuredOutput` tool call and
surfaces the result as a `structured_output` object on the `result` envelope, which
`harness.Result.StructuredOutput` carries verbatim as `json.RawMessage` (see fixture
`06_json_schema.jsonl` and `testdata/PROTOCOL.md`). Consequences: Styr never parses or validates
the schema itself — it trusts the CLI's own enforcement — so a malformed or missing
`structured_output` (e.g. schema unset, or the model failing to call the tool) must be handled
by callers (the run engine, per the v0.2 plan) as a report-less run rather than assumed present;
`--append-system-prompt` has no observable effect in the wire protocol, so it is trusted
uncritically and never asserted against transcript content.

## ADR-010: One shared-secret scheme for every inbound trigger
Date: 2026-09-18. Status: accepted.

Context: `POST /hooks/{slug}` must authenticate senders that Styr does not control. The v0.2 plan
sketched GitHub's own scheme — `X-Hub-Signature-256`, an HMAC-SHA256 of the raw body keyed with
the shared secret — for `github` triggers. Verifying that HMAC needs the raw secret, but Styr
deliberately stores only its sha256 (the secret is shown once at creation and at rotation, like
an API token), so it cannot compute the MAC. The alternatives were to seal the secret with the
`crypto.Box` in a new column (the 00003 migration is already merged, and a reversible webhook
secret is a strictly weaker store than a hash), or to key the HMAC with the stored hash and tell
operators to paste that hash into GitHub (correct, but confusing and non-standard).

Decision: every trigger kind, `github` included, authenticates with the same bearer check —
sha256 of the presented secret compared in constant time against the stored hash. The secret may
arrive in any of three carriers: `X-Styr-Secret`, `Authorization: Bearer <secret>`, or a `secret`
query parameter. The query parameter exists because GitHub webhooks (and some appliances) cannot
add custom headers; for `github` triggers the documented setup is a payload URL of
`https://<styr>/hooks/<slug>?secret=<secret>`, with GitHub's own secret field left empty.

Consequences: Styr never verifies `X-Hub-Signature-256` and a GitHub webhook body is therefore
not integrity-checked — the secret in the URL is the whole authentication, so a trigger's URL is
as sensitive as its secret and rotation (`POST /triggers/{id}/rotate-secret`) is the remedy for a
leaked one. Secrets in URLs can land in proxy and access logs, which is why the header carriers
stay the documented default for everything that can send them. The scheme is uniform, so the
router has one code path and the UI has one setup story; if a future kind genuinely needs
signature verification, it gets a sealed secret column of its own rather than changing this one.

## ADR-011: Per-session model and effort, hidden built-ins
Date: 2026-09-18. Status: accepted.

Context: v0.2 lets a profile default a session's model and reasoning effort, and lets an operator
change either mid-session from the session header, but the CLI only accepts `--model`/`--effort`
as start-up flags — nothing in the wire protocol changes them on a running process. Separately,
the composer's `/` menu needed to know which of the CLI's own `slash_commands` (the init
message's flat, undifferentiated list of custom commands, plugin skills and built-ins) are safe
to offer in Styr's headless (`-p`) pipe rather than an interactive terminal. Card T38 spiked both
questions against the real CLI 2.1.276 (three short recorded sessions, one command each,
documented in `internal/harness/claude/testdata/PROTOCOL.md`'s "Built-in slash commands in `-p`
mode").

Findings: `/compact` and `/cost` are answered directly by the CLI as a synthetic assistant text
block plus a zero-turn, zero-cost `result` — the session id and transcript are untouched, so
both work over the pipe exactly like a normal turn. `/clear`, by contrast, resets the
conversation and re-inits under a **new** session id (a `conversation_reset` message the codec
doesn't model, followed by a second `system`/`init`); since Styr resumes a session by its own
stored id and keeps its own event log keyed to it, a `/clear` would silently orphan both with no
signal the UI could show. `doctor`, `color` and `reload-plugins` are reported by the CLI itself as
terminal-only, via the init message's separate `terminal_slash_commands` array.

Decision: sessions gain a per-session `model`/`effort` (profiles gain matching defaults,
migration `00004_model_effort.sql`); switching either (`POST /sessions/{id}/model`) closes the
live process and resumes the same session id under the new flags rather than attempting an
in-place change. `internal/harness/claude/builtins.go` hard-codes two classifications rather than
inferring them from the init message: `HeadlessBuiltins()` (`compact`, `cost`, safe to offer) and
`HiddenBuiltins()` (`clear`, `doctor`, `color`, `reload-plugins`, plus `model` and `effort`
themselves — hidden not because they fail over the pipe but because Styr already owns them
through the header selects, and letting the CLI change them behind Styr's back would desync the
stored values). `GET /status` exposes `hidden_commands` so the composer's slash menu
(`web/src/components/session/slashCommands.ts`) can filter the session's own `slash_commands`
list without re-deriving the spike's findings client-side.

Consequences: the menu is built from real, CLI-reported command names rather than a maintained
allowlist, so a newly installed plugin skill or custom command shows up automatically — at the
cost of trusting a hard-coded hidden-set that only a future spike (re-run when the CLI's headless
behaviour for these commands changes) can safely update. A model or effort switch always pays the
cost of a process restart (no first message is sent until the operator's next turn), which is
accepted as the price of the CLI's start-up-flags-only design rather than attempting a live
in-process reconfiguration the protocol doesn't support.

## ADR-012: Plan approval over the permission channel

Date: 2026-09-18. Status: accepted.

Context: the v0.3 "Review" design left open whether a `plan`-mode session's finished plan arrives
as some distinct message type, and — the harder question — whether approving it lets the CLI
keep going in the same process or requires Styr to resume with `--permission-mode default`. Card
T41 spiked both against the real CLI 2.1.276: one recorded session in `/tmp/styr-fixture-ws`
(`hack/recorder/main.go -mode plan`, prompt asking for a plan to add a `--version` flag to a
small Go CLI, then to exit plan mode), kept as
`internal/harness/claude/testdata/08_plan_mode.jsonl`; full detail in `testdata/PROTOCOL.md`
("Plan mode in `-p`").

Findings: `ExitPlanMode` is not a distinct message type — the model calls it as an ordinary tool,
so it surfaces as the same `control_request`/`can_use_tool` control message every other tool
permission uses, with the plan markdown at `request.input.plan` and `request.tool_name ==
"ExitPlanMode"`. Approving it (an ordinary `allow` `control_response`) does **not** end the
process: the CLI switches its own `permissionMode` from `plan` to `default` (observed as a
`system`/`status` line immediately after the response) and the model continues acting on the
plan in the same process, under the same session id, asking permission for each subsequent tool
call exactly as before — no `--resume`, no second `init`. A second spike (resuming a
plan-mode-stopped session under `--permission-mode default`) was therefore unnecessary and not
run, per the card's own fallback rule. A separate, one-off spike (not kept as a fixture; see
`testdata/PROTOCOL.md` "Checkpoint safety between turns") confirmed that committing the
workspace's git state between two turns of the same live session (`git add -A && git commit`,
run via a new recorder `-between` flag) does not disturb the next turn — the CLI only reads
files off disk per tool call, never git state.

Decision: Styr treats `ExitPlanMode` as a permission request like any other, routed to a Plan
card instead of the ordinary tool-approval UI (`claude.IsPlanExit(harness.PermissionRequest)
bool` checks `ToolName == "ExitPlanMode"`; `harness.PermissionRequest` gains a `Plan string`
field, populated by the codec from `request.input.plan` only for that tool). "Approve plan" is
exactly `Process.Decide(Decision{RequestID: ..., Allow: true})` on that request — nothing else;
the same running process carries on. "Request changes" is `Decide{Allow: false, Message: <the
typed comment>}`. `internal/gitops`'s planned per-turn checkpoint commit needs no special-casing
around a pending or just-approved plan: it is safe to checkpoint between any two turns of a live
session regardless of what happened in between.

Consequences: the sessions service (T42) does not need a resume-on-approve code path at all,
simplifying the plan-approval flow to a single `Decide` call plus a UI re-render when the next
event arrives. The tradeoff is that "approve" and "let it start editing" are the same action —
Styr cannot approve a plan without also, in effect, un-pausing implementation, matching the real
CLI's own coupling of the two rather than trying to add a pause point the protocol doesn't offer.

## ADR-013: Styr-managed worktrees and per-turn checkpoints

Date: 2026-09-18. Status: accepted.

Context: the CLI itself already accepts a `--worktree <name>` flag (`harness.StartSpec.Worktree`,
`internal/harness/claude/harness.go`'s `BuildArgs`) — it will create and manage a git worktree
for the process on its own. v0.3's Review tab, checkpoints, commit and PR flow all need to know
exactly where that worktree lives on disk *before* the CLI process even starts (to set the
process's `cwd` to it, per ADR-001's `-p` model of one process per session) and need to run
ordinary `git`/`gh` commands against it independently of any live CLI process (a diff or a
commit while the session is idle between turns, a rewind or discard while it's closed). The
CLI's own `--worktree` only gives it a *name*, not a resolvable host path — Styr would have to
either guess or reverse-engineer where the CLI put it, and would have no way to create the
worktree ahead of starting the process (defeating "cwd is the worktree from the first turn") or
to touch it while no process is running.

Decision: Styr manages worktrees itself via a new `internal/gitops` package that drives `git`
directly (`git worktree add -b <branch> <dir> <base>`, plain `os/exec`, no shell), never passing
`--worktree` to the CLI. Styr picks the path (`<workspace>/.styr/worktrees/<session id>`) and
the branch name before creating anything, so both are known and stable for the whole life of the
session — Review, checkpoint, commit, rewind, discard and PR operations all address the worktree
by that same path regardless of whether a CLI process is currently running against it. A
`--worktree`-based alternative would need the CLI's cooperation (and its own control-protocol
surface, none of which is documented) for anything Styr needs to do to that directory outside a
live turn.

This only works because a `T41` spike answered the question a checkpoint-between-turns scheme
depends on: does committing the workspace's git state *while a CLI process has it open* disturb
that process? It does not — a `git add -A && git commit` run between two turns of the same live
session left the next turn unaffected (`internal/harness/claude/testdata/PROTOCOL.md`,
"Checkpoint safety between turns"; the CLI only reads files off disk per tool call, never git
history or the index). That is what makes `internal/gitops.Worktree.Checkpoint` safe to run
after every turn, unconditionally, without coordinating with the harness process at all.

`Checkpoint` commits under a fixed `Styr <styr@local>` identity, one per turn that actually
changed something (a clean diff commits nothing); `Commit` — the explicit "publish this" action
— then folds every checkpoint commit made since the session's `base_ref` into one commit with
`git reset --soft base_ref` before committing the working tree fresh under the *calling user's*
identity. The checkpoint trail is disposable bookkeeping for Rewind, never the artifact that
leaves the session: nothing downstream (a PR, a merge to `base_branch`) ever sees a
"styr: checkpoint after turn N" commit on its own.

Consequences: Styr owns one more moving part (`internal/gitops`, a package of `git`/`gh`
invocations to keep in sync with whatever git version operators run) that a `--worktree` flag
would have handed to the CLI, but in exchange gets a worktree path it controls fully — including
before the process starts and while the session is closed — and a checkpoint scheme with no
coupling to the harness protocol at all. `harness.StartSpec.Worktree` stays in the type for a
future harness that might genuinely need it, but `internal/sessions` never sets it.

## ADR-014: Loops iterate on one session

Date: 2026-09-19. Status: accepted.

Context: v0.4's loop feature repeats a template's run until its structured report says the work
is done. The design question was whether each iteration should be a fresh session (`--resume`
under a new session id, or a wholly new one), giving every iteration a clean slate and its own
line-item cost, or whether every iteration should be a turn sent to the *same* session the loop
started, so the model keeps the whole prior conversation in context.

Decision: a loop's iterations are all turns on one session (`internal/runs/loops.go`'s
`nextIteration` calls `sessions.Send` on the loop's `SessionID`, never `sessions.Create`). The
next iteration's prompt is the template re-rendered plus an explicit "Iteration N of M. Previous
report: `<json>`" prefix (see `docs/SCHEDULES.md`), rather than relying on the model's memory of
its own prior turn alone — but that prior turn, and everything it did (every tool call, every
file it read or wrote), is still genuinely present in the session's context, not summarised or
discarded.

Consequences: this is the whole point for a repeat-until-correct workload (fix the tests, keep
iterating until green) — a fresh session would force the model to rediscover the codebase's state
from scratch every iteration, at real token cost, with no guarantee it converges on the same
understanding twice. The tradeoff is cost visibility: **a loop's cost accrues on one session**,
so there is no per-iteration cost line, only the session's running total — a loop that goes
through all 5 default iterations looks like one (possibly expensive) session on the cost
dashboard, not five separate charges, and the origin breakdown attributes it to `loop` as a
whole rather than to an iteration. A known gap follows from reusing one session rather than
persisting loop state of its own: the variables a loop's first iteration was rendered with live
only in an in-process cache (`varsCache`, keyed by loop id), not in the `loops` table — a
`styr serve` restart while a loop is `running` loses them, so a prompt rendered after restart
would substitute empty values for any of the original vars a later iteration's template
references. This is accepted for v0.4 rather than adding a persisted vars column: a loop stuck
`running` across a restart is not automatically resumed at all (nothing re-drives it), so the gap
only bites an operator who manually nudges a stale loop rather than stopping it — `docs/
SCHEDULES.md`'s "Loop variables are held in memory only" documents the operational consequence
directly rather than leaving it to be discovered.

## ADR-015: Cancel request contexts before HTTP shutdown

Date: 2026-09-19. Status: accepted.

Context: `styr serve`'s graceful shutdown (SIGINT/SIGTERM) must stop accepting new work and let
in-flight work finish inside a bounded `shutdownTimeout` (15s). `GET /api/v1/events` (the SSE
feed every open browser tab holds one of, per ADR-005) is a handler that runs indefinitely by
design — it only returns when its request context is cancelled, since it has no other stopping
condition. `http.Server.Shutdown` on its own closes idle listeners and waits for in-flight
handlers to *return*, but does nothing to make a still-blocked handler return sooner; a live SSE
tab left open at shutdown time would therefore make `Shutdown` block for the entire
`shutdownTimeout` and then still be forcibly cut off, rather than closing promptly.

Decision: `runServe` builds one long-lived `reqCtx` (`context.WithCancel(context.Background())`)
and wires it as `http.Server.BaseContext`, so **every** request's context — SSE included —
derives from `reqCtx` rather than from the per-connection context `net/http` would otherwise
supply on its own. On shutdown, `reqCancel()` is called *before* `srv.Shutdown(shutCtx)`:
cancelling `reqCtx` ends every open SSE stream (and unblocks anything else selecting on its
request context) immediately, so by the time `Shutdown` starts waiting for handlers to return,
the ones that were only ever blocked on the stream have already finished. `sessionsSvc.Shutdown`
(closing every open harness child process) runs after `srv.Shutdown` returns, under its own
15s timeout — HTTP and the sessions service each get the full grace period rather than sharing
one budget.

Consequences: shutdown is now two ordered phases (cancel request contexts, then wait for HTTP;
then close harness processes) instead of one `Shutdown` call — a future long-lived handler (a
websocket, a different streaming response) gets the same clean-cancellation behaviour for free
as long as it derives its lifetime from the request context rather than `context.Background()`
directly, which is now a documented expectation rather than an accident of one endpoint's
implementation. The v0.4 scheduler and loop-advancement goroutines are unaffected by this
ordering — they stop via their own `maintCtx` cancellation, independent of `reqCtx` — but a
schedule's own "Run now" HTTP call, and any browser tab watching a loop's session over SSE, both
depend on this ordering to unblock promptly rather than waiting out the full timeout.

## ADR-016: Pipelines advance on run events

Date: 2026-09-19. Status: accepted.

Context: v0.5's pipelines chain several template runs into a DAG — a step starts once every step
it `needs` has succeeded, a failed step retries within a budget, and a `foreach` step fans out
into N parallel step-runs. The design question was how the pipeline executor learns that one of
its steps' runs has finished, to decide what to start next: poll the `runs` table on a timer, add
a callback/hook the run engine calls directly, or reuse the same event bus `internal/runs.Engine`
already publishes session events on.

Decision: `internal/runs.Engine`'s `finish` — the single place every run closes out, whatever
outcome ends it (`internal/runs/loop.go`) — now publishes `run.finished` (success) or
`run.failed` (every other outcome) on the bus, carrying `{run_id, step_run_id, outcome}`.
`runs.RunInput` gains a `StepRunID` field, set only when the run is one attempt of a pipeline
step and carried onto the `runs` row (migration `00008_pipelines.sql`'s `runs.step_run_id`
column); every other run publishes a nil `step_run_id`. `internal/pipelines.Executor.Run`
subscribes to the same bus (structurally identical to `runs.Engine.Run`'s own subscribe-and-select
loop) and reacts only to messages carrying a non-nil `step_run_id`: it loads the step run,
records the report or the failure, and advances the graph — starting the next eligible step(s),
expanding a fan-out whose dependency just succeeded, or settling the pipeline run out once
nothing is left running. A 30s sweep (the same interval the scheduler and run-engine timeouts
use) closes out pipeline runs that overran their definition's timeout and reconciles any step-run
whose bus message never arrived (a dropped message, or a restart between the run finishing and
the message being handled) by re-reading its underlying `runs` row directly.

Consequences: the pipeline executor needed no new coupling to `internal/runs` beyond the bus it
already fans every session and run event through, and no polling loop of its own beyond the
timeout/reconcile sweep every other unattended-work package (`internal/runs`, `internal/
schedules`) already has. A step's `runs.Engine.Start` call is an ordinary run start in every other
respect — same template rendering, same session creation, same notification path — which is what
lets a pipeline step's run show up on the Runs page, the cost dashboard and the fleet Gantt
exactly like any other run, with `origin: pipeline` as its only tell. The tradeoff is the same one
`internal/runs`' own reconciliation already accepts: a bus message can be dropped (a full
subscriber channel, a process restart), so correctness cannot depend on every message arriving —
the 30s sweep's reconciliation is what makes a dropped message a bounded delay rather than a stuck
pipeline, at the cost of the executor also needing that sweep's more complex "is this really still
running" check rather than trusting the bus alone.

## ADR-017: Nullable trigger and schedule targets; single-connection migrations

Date: 2026-09-19. Status: accepted.

Context: v0.5 lets a trigger or a schedule start either a template run or a pipeline run
(`template_id`/`pipeline_id`, exactly one). Migration `00008_pipelines.sql` added the
`pipeline_id` columns to `triggers` and `schedules`, but both tables' `template_id` column was
still `NOT NULL REFERENCES templates(id)` from earlier migrations (`00003_triggers.sql`,
`00006_schedules.sql`), leaving no legal value to store for a row that targets a pipeline instead
of a template. SQLite has no `ALTER TABLE ... ALTER COLUMN` to drop a `NOT NULL` constraint in
place; the only supported way to change a column's constraints is SQLite's own documented
12-step procedure: turn foreign keys off, create a replacement table with the new schema, copy
every row across, drop the old table, rename the replacement into its place, turn foreign keys
back on.

Decision: migration `00010_pipeline_targets.sql` runs that rebuild for `triggers` and
`schedules`, relaxing `template_id` to nullable on both (the foreign key itself is kept — a
non-null `template_id` must still reference a real template) and re-creating `schedules`' index
afterwards. `PRAGMA foreign_keys` is both a per-connection setting and a documented no-op inside a
transaction, so this migration is marked `-- +goose NO TRANSACTION` and `internal/db.migrate`
opens goose's own migration runner on a `*sql.DB` capped at exactly **one** connection
(`migrator.SetMaxOpenConns(1)`), closed again before `Open` hands the application's own
(unrestricted) pool back — a second pooled connection borrowed mid-migration would not see the
same session's `PRAGMA foreign_keys = OFF`, and could reintroduce the very constraint enforcement
the rebuild is trying to work around.

Consequences: every future migration that needs a `PRAGMA` toggle (`foreign_keys`, or any other
per-connection pragma) gets the same single-connection guarantee for free, rather than each one
having to reason about pool behaviour itself — the constraint is now structural (`db.migrate`'s
own pool), not a convention migration authors have to remember. The cost is that migrations run
serialized on one connection rather than whatever concurrency the driver's pool would otherwise
allow, which is immaterial at Styr's scale (one process, migrations run once at startup) but would
need revisiting if migrations ever needed to run concurrently against a large existing database.
`internal/triggers` and `internal/schedules` both gained an `exactlyOneOfTemplateOrPipeline`
validation rule at the API layer to keep the "exactly one of the two" invariant the nullable
columns no longer enforce at the schema level (a `CHECK` constraint expressing "at least one of
two nullable columns is non-null, but not both" is possible in SQLite but was judged not worth the
same rebuild machinery for a rule the handlers already had to validate for the 422 error message
anyway).
## ADR-018: Codex harness: sandbox policy instead of per-command approvals

Date: 2026-09-19. Status: accepted.

Context: Styr's `Harness` interface was designed around the Claude Code CLI, whose
`--permission-prompt-tool stdio` turns every risky tool use into a `can_use_tool`
`control_request` that the host answers with an allow/deny `control_response` — that round trip
is what the inbox's approve/deny affordance and `Process.Decide` exist for. The OpenAI Codex
CLI (`codex-cli 0.154.0`, spiked in T60, see `internal/harness/codex/testdata/PROTOCOL.md`) has
no equivalent. `codex exec` offers exactly three permission postures: a `--sandbox` policy
chosen once when the process starts (`read-only`, `workspace-write`, `danger-full-access`),
`--approve-for-me` (approvals answered by an automatic reviewer inside the CLI, with no host
channel), and two bypass flags — one skipping approvals and sandboxing outright, one running
hooks without persisted trust — that Styr never passes. Nothing in the
`--json` event stream ever asks the host a question, and there is no stdin channel to answer on
— `codex exec` takes one prompt as an argument and exits when the turn ends.

Decision: a Codex session's permissions are expressed entirely as a sandbox policy derived from
the session's profile at process start, and `Process.Decide` returns the new sentinel
`harness.ErrUnsupported` rather than pretending to approve anything. `codex.SandboxMode` maps
`plan` and `dontAsk` — and any profile that disallows both `Edit` and `Write`, which is how the
`investigate` profile says "look, do not touch" — to `read-only`; every other mode
(`default`, `acceptEdits`, `auto`) to `workspace-write`, i.e. writes confined to the session's
own workspace directory. `danger-full-access` is never emitted, and neither bypass flag appears
anywhere in the package or the recorder. Because `codex exec resume` accepts no `--sandbox`
flag at all, a resumed turn re-states the policy as a `-c sandbox_mode=<mode>` config override
rather than inheriting whatever the CLI's default happens to be — the `investigate` profile has
to stay read-only on turn five as much as on turn one.

Consequences: `ErrUnsupported` is a normal outcome, not a failure. The UI must hide the
approve/deny affordances for a Codex session rather than showing them and failing, and the
session's inbox should say plainly that Codex sessions apply a sandbox policy instead of
per-command approvals — an operator who grants `default` to a Codex session is granting write
access to the whole workspace for the whole session, not a chance to review each command. The
two harnesses are therefore not interchangeable at the same trust level: the same profile buys
finer-grained control under Claude Code than under Codex, and the honest place to say so is the
harness chip and the profile documentation. `Interrupt` also changes meaning: with no control
channel, it kills the turn's OS process, so the turn ends with a synthesised `interrupted`
result rather than with the CLI's own acknowledgement.

## ADR-019: Viewer role and workspace access lists

Date: 2026-09-19. Status: accepted.

Context: v1.0 adds a handful of real, distinct users on the same box rather than just the
operator. Two gaps followed from that: some people should be able to watch — sessions, runs,
the cost dashboard, the fleet Gantt — without being able to start a session, decide an
approval, or edit a template, trigger or pipeline; and some shared (owner-less) workspaces
should not be usable, or even visible, to every signed-in user, the way `member` visibility
worked up to v0.5.

Decision: a third role, `viewer`, sees exactly what `member` sees but cannot change anything
except its own account — profile prefs, personal API tokens, its own Claude/Codex credentials.
Rather than adding a role check to every handler that mutates state, `internal/auth.RequireWriter`
is a single generic guard `internal/api/middleware.go`'s `writerGuard` applies to every route
*except* an explicit three-route allowlist (`PATCH /me` and the caller's own token routes) —
opt-out of the guard, not opt-in per handler, so a new mutating route can't forget it. Separately,
a shared workspace gets its own `access` column (`everyone` default, or `listed`) and a
`workspace_access` join table (migration `00012_workspace_access.sql`); `listed` restricts both
visibility and session creation to the named users plus every admin, and an unlisted caller gets
the same `404` a nonexistent workspace would, consistent with the existing "visibility is not
leaked" rule in `docs/API.md`'s error envelope. Unattended starts (a webhook, schedule or
pipeline step) always run as the internal service actor and are never blocked by a `listed`
allowlist — there is no signed-in caller to check it against. The first user to ever sign in is
still always `admin` and can never be demoted, by themselves or another admin, so a role mistake
always has someone left able to fix it.

Consequences: a single cross-cutting guard means `viewer` enforcement cannot be forgotten on a
future mutating endpoint the way a per-handler check could be; the cost is that the three
exceptions must be kept in sync by hand as the route table grows, rather than being derivable
from the routes themselves. Access lists are scoped to *shared* workspaces only — a workspace a
member owns personally has no `access` column and is unaffected — which keeps the migration and
the UI (an admin-only "Access" control on Workspaces) small at the cost of not yet covering
per-workspace access for a personally-owned workspace someone wants to share, a gap left for a
later card if it turns out to matter.

## ADR-020: API v1 compatibility guard in CI

Date: 2026-09-19. Status: accepted.

Context: `docs/openapi.yaml` reached `info.version: 1.0.0` with the v1 freeze (T62), and
`docs/API.md` promises that, within the `1.x` line, the API only changes additively: no removed
or retyped response field, no newly required request field, no removed operation. A promise
like that is only as good as its enforcement — without a check, a later PR could drop a field or
tighten a request body and nothing would fail until an external client broke against a `1.x`
release that was supposed to be safe to upgrade to.

Decision: `hack/openapi-compat` is a small Go program (`compare.go`, `main.go`) that loads two
OpenAPI documents and fails when it finds any of the four breaking shapes the promise rules out:
a removed path or operation, a removed response schema property, a retyped response schema
property, or a new required request-body field on an operation that already existed. `make
api-compat` runs it as `git show <last v* tag>:docs/openapi.yaml` against the working tree's
copy, skipping with a notice (not a failure) when no `v*` tag is reachable yet — a fresh
checkout, or a repository before its first release. CI's backend job runs `make api-compat`
immediately after `make check`, and fetches tags first (`a2e0614`) so the comparison always has
a previous release to diff against rather than silently comparing against nothing.

Consequences: the compatibility promise is enforced mechanically at every PR, not just at
review time, and a violation fails with the specific field or operation named rather than a
generic reminder to check the docs. The guard is deliberately narrow — it only catches the four
structural shapes above, not every way a field's *meaning* could quietly change while keeping
its declared type and requiredness — so it complements the review a genuinely new endpoint or
field still needs rather than replacing it. Running the comparison against the previous release
*tag* rather than the previous commit on `main` also means the guard is a property of what
actually shipped, not of in-progress work still on the default branch between releases.
