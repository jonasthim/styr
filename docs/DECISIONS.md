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
