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
