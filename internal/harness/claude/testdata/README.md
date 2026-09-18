# Fixtures

Recorded 2026-09-18 by `hack/recorder/main.go` (Task 2 spike) against the real, logged-in
`claude` CLI 2.1.276 on the workstation (Max subscription, no `ANTHROPIC_API_KEY`), working in a
throwaway git repo at `/tmp/styr-fixture-ws`. Each line is exactly one stdout line emitted by
the CLI; lines prefixed `>>> ` record what the recorder wrote to the CLI's stdin. See
`PROTOCOL.md` for the field-by-field contract these fixtures establish.

- `01_simple_text.jsonl` — single turn, plain text reply, no tools. Recorded successfully.
- `02_tool_read.jsonl` — single turn that uses the `Read` tool, pairs `tool_use`/`tool_result`.
  Recorded successfully.
- `03_permission_bash.jsonl` — single turn that uses the `Bash` tool, intended to exercise
  `control_request`/`control_response` for a permission prompt. **No `control_request` was
  observed** in either the `--permission-mode default` run or the `--permission-mode manual`
  retry; the file on disk is the kept `manual`-mode recording. See `PROTOCOL.md` for the full
  analysis and the fallback question left for the human.
- `04_multi_turn.jsonl`, `05_interrupt.jsonl` — **not recorded.** Per the task card's rule for
  fixture 03 ("if still none, STOP, commit what you have, and report"), recording stopped after
  the fixture-03 retry failed to produce a `control_request`, to leave the remaining session
  budget for a follow-up spike once the human has decided the fallback.
- `07_slash_compact.jsonl` — the `/compact` built-in sent as a user turn (card T38 spike),
  trimmed to the stdin line, `init`, the CLI's synthetic text reply and the `result`. See
  `PROTOCOL.md`, "Built-in slash commands in `-p` mode", for what `/cost` and `/clear` did; those
  two transcripts were deliberately not kept (real usage figures, and hook noise respectively).
