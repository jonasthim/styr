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
