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
