# Observed `codex exec --json` protocol — codex-cli 0.154.0

Recorded 2026-09-19 by `hack/codex-recorder/main.go` against the real, logged-in workstation
CLI (`codex login status` → "Logged in using ChatGPT", no `OPENAI_API_KEY` set). Command lines
used, one process per fixture:

```
codex exec --json -C /tmp/styr-codex-ws --sandbox read-only       "Reply with exactly the word pong."          # 01
codex exec --json -C /tmp/styr-codex-ws --sandbox read-only       "Read README.md and reply with ..."          # 02
codex exec --json -C /tmp/styr-codex-ws --sandbox workspace-write \
           --output-schema /tmp/codex-schema-*.json               "Create a file hello.txt containing hi, ..."  # 03a, 03
codex exec resume 01a0b707-ce58-7660-b301-4939ce14c766 --json     "What word did you reply with before? ..."   # 04
```

The workspace was a throwaway git repository (`git init`, one committed `README.md`) at
`/tmp/styr-codex-ws`, per the card's rule that no real Codex session touches a Styr checkout.

## Fixtures

| file | what it records | exit |
| --- | --- | --- |
| `01_simple_text.jsonl` | one assistant message, no tools, sandbox `read-only` | 0 |
| `02_read_file.jsonl` | a shell command item (`head -n 1 README.md`) and its output | 0 |
| `03a_schema_rejected.jsonl` | `--output-schema` **rejected by the API**: `error` + `turn.failed` | 1 |
| `03_write_schema.jsonl` | `--sandbox workspace-write` + `--output-schema`, file created, JSON final answer | 0 |
| `04_resume.jsonl` | `codex exec resume <thread id from 01>` with a prompt | 0 |
| `05_resume_missing.jsonl` | a resume of an unknown thread: **no stdout at all**, error on stderr | 1 |

Each `NN_*.jsonl` is the child's stdout verbatim with one appended line `>>> exit N` (the
process exit code); the matching `NN_*.jsonl.stderr` is the child's stderr verbatim.

Two hand edits were made to the recordings, both noted here because nothing else was touched:

- `03_write_schema.jsonl`: the `aggregated_output` of items `item_2` and `item_3` was replaced
  with a `[redacted ...]` string. The model, unprompted, `cat`-ed local skill files and the
  operator's personal notes, and that content must not enter an open-source repository. The
  field is a plain JSON string either way, so the codec sees the same shape.
- `03a_schema_rejected.jsonl` is the rejected first attempt at fixture 03, kept because it is
  the only recording of the `error` and `turn.failed` envelopes. See "Output schemas must be
  strict" below.

**Session budget.** The card allowed four real `codex exec` sessions. Four model turns were
run (01, 02, 03, 04). The fifth invocation (`03a`) is *not* counted as one of them: the API
rejected the request with HTTP 400 before the model ran, so it produced no turn — it is a
recording of an argument mistake, not of a session. `05_resume_missing.jsonl` likewise never
reached the API (the CLI failed the thread lookup locally).

## Envelope types

Every stdout line is one JSON object with a top-level `"type"`. The complete set the exec
front end can emit, read off the CLI binary's serde symbols, is:

`thread.started`, `turn.started`, `turn.completed`, `turn.failed`, `item.started`,
`item.updated`, `item.completed`, and a bare `error`.

### `thread.started` — always the first line, carries the session id

```json
{"type":"thread.started","thread_id":"01a0b707-ce58-7660-b301-4939ce14c766"}
```

`thread_id` is **the** session identifier: it is what `codex exec resume <id>` takes, and it is
what Styr stores as the session's harness id. A resume re-emits `thread.started` with the *same*
`thread_id` (fixture 04, line 1 — identical to fixture 01, line 1), so the host can key on this
line for both a fresh turn and a resumed one.

The stream carries **no model name anywhere** — not on `thread.started`, not on any item, not
on `turn.completed`. A host that wants to show which model answered has to remember what it
passed as `-m`. (Styr's codec therefore takes the model as a parameter and echoes it back on
`EventInit`.)

### `turn.started` — no payload

```json
{"type":"turn.started"}
```

### `item.started` / `item.completed` — the body of a turn

Both wrap a single `item` object with `id` (`item_0`, `item_1`, … restarting from `item_0` on
every process, including a resume) and `type`. `item.updated` exists in the binary but was
never emitted in these recordings.

Item types the binary knows: `agent_message`, `reasoning`, `command_execution`, `file_change`,
`mcp_tool_call`, `web_search`, `todo_list`, `error`, plus collab/sub-agent variants. Four were
observed.

**`agent_message`** — assistant text. Only ever seen as `item.completed`; there are no token
deltas in exec mode (nothing equivalent to Claude Code's `stream_event`/`text_delta`, so a
Codex session in Styr has no partial-text streaming).

```json
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"pong"}}
```

**`command_execution`** — a shell command the model ran, as a `started`/`completed` pair with
the same `item.id`. `command` is the full argv as one string (the CLI wraps it in
`/usr/bin/bash -lc '…'`), `aggregated_output` is stdout+stderr interleaved, `exit_code` is
`null` while in progress, and `status` is `in_progress` then `completed` (`failed` exists).

```json
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/usr/bin/bash -lc 'head -n 1 README.md'","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/usr/bin/bash -lc 'head -n 1 README.md'","aggregated_output":"styr codex fixture workspace\n","exit_code":0,"status":"completed"}}
```

**`error`** — an informational error the CLI reports as an item rather than as a failure. Every
fixture starts with one, caused by the operator's own `~/.codex` hook configuration, because
the recorder does not isolate `HOME` for the child:

```json
{"type":"item.completed","item":{"id":"item_0","type":"error","message":"clamping SessionEnd hook timeout to 3s in /home/thim/.codex/plugins/cache/openai-codex/codex/1.0.6/hooks/hooks.json"}}
```

A production Styr session gives the child an isolated `HOME`, so this particular item should
not appear there. It does **not** end the turn — `turn.completed` still follows.

**`file_change` — not observed.** Asked to create `hello.txt`, the model used a shell
redirection (`printf hi > hello.txt`) rather than the patch tool, so fixture 03 contains no
`file_change` item; a workspace-write turn can therefore write files while emitting nothing but
`command_execution`. The shape below is read off the CLI binary's serde metadata
(`struct FileChangeItem with 6 elements`: `changes`, `status`, `auto_approved`, `stdout`,
`stderr` alongside `id`/`type`; `struct FileUpdateChange with 3 elements` including `move_path`;
the change-kind enum is `add` | `delete` | `update`) and is **unverified against a real line**:

```json
{"type":"item.completed","item":{"id":"item_N","type":"file_change","changes":[{"path":"hello.txt","kind":"add"}],"status":"completed"}}
```

Styr's codec decodes `changes` defensively: an array of objects, taking whichever of `path`,
`file_path` or `move_path` is present, and ignoring the item when it can find no path at all.
Re-record a fixture with a real `apply_patch` turn before relying on it.

### `turn.completed` — success, with token usage and nothing else

```json
{"type":"turn.completed","usage":{"input_tokens":19234,"cached_input_tokens":12160,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}
```

Fields: `input_tokens`, `cached_input_tokens`, `cache_write_input_tokens`, `output_tokens`,
`reasoning_output_tokens`. **There is no cost field and no duration field** — nothing like
Claude Code's `total_cost_usd`/`duration_ms`. Styr reports a Codex turn's cost as `0`; the
inbox must not present that as "this turn was free".

### `turn.failed` and the bare `error` envelope

A failing turn emits a top-level `error` line and then `turn.failed`, both carrying the same
message, and the process exits **1**. From `03a_schema_rejected.jsonl`:

```json
{"type":"error","message":"{\n  \"type\": \"error\",\n  \"error\": {\n    \"type\": \"invalid_request_error\",\n    \"code\": \"invalid_json_schema\",\n    \"message\": \"Invalid schema for response_format 'codex_output_schema': In context=(), 'additionalProperties' is required to be supplied and to be false.\",\n    \"param\": \"text.format.schema\"\n  },\n  \"status\": 400\n}"}
{"type":"turn.failed","error":{"message":"{\n  \"type\": \"error\", … same body … }"}}
```

Note the two different shapes: the bare envelope has a top-level `message` string, `turn.failed`
nests it under `error.message`. The message body is whatever the upstream API returned, often
itself a JSON document as a string.

A failure can also produce **no stdout at all**. `05_resume_missing.jsonl` is empty apart from
the recorder's `>>> exit 1`, and the reason is only on stderr:

```
Error: thread/resume: thread/resume failed: no rollout found for thread id 00000000-0000-0000-0000-000000000000 (code -32600)
```

So a host must treat "process exited non-zero without a `turn.completed`/`turn.failed`" as a
failed turn and surface the last stderr line; Styr's `process.go` synthesises the result event
in exactly that case.

Stderr is otherwise noise, not a channel: it carries `Reading additional input from stdin...`
and the MCP servers' connection errors (`rmcp::transport::worker: worker quit with fatal: …`)
even on a completely successful run. Only the last line is worth keeping.

## Structured output: an ordinary final message

`--output-schema <file>` takes a **path to a file** containing the JSON Schema — not the schema
itself, unlike Claude Code's `--json-schema`. The constrained answer is **not** delivered on a
dedicated field or event and **not** written to a file: it arrives as the turn's last
`agent_message` item, whose `text` is the JSON document (fixture 03, `item_6`):

```json
{"type":"item.completed","item":{"id":"item_6","type":"agent_message","text":"{\"severity\":\"info\",\"diagnosis\":\"Created hello.txt and verified it contains hi.\"}"}}
```

Consequence for the codec: `StructuredOutput` cannot be produced from a single line in
isolation — the decoder has to remember the last `agent_message` of the turn and attach it to
the result when a schema was requested. (`-o/--output-last-message FILE` would additionally
write that same last message to a file; Styr does not use it, since the text is already on the
stream.)

### Output schemas must be strict

The card's schema was rejected verbatim. The API requires `"additionalProperties": false` on
every object in the schema:

> Invalid schema for response_format 'codex_output_schema': In context=(), 'additionalProperties'
> is required to be supplied and to be false.

The recorded fixture therefore uses

```json
{"type":"object","additionalProperties":false,"required":["severity","diagnosis"],
 "properties":{"severity":{"enum":["info","warning","critical"]},"diagnosis":{"type":"string"}}}
```

This is a user-facing constraint: a Styr profile's JSON schema that works with the Claude
harness may be rejected by the Codex one, and the rejection arrives as a failed turn (exit 1)
rather than as a startup error.

## One process per turn — confirmed

`codex exec` takes exactly one prompt, as the positional `PROMPT` argument, and the process
exits when the turn ends (every fixture ends with `turn.completed`/`turn.failed` and then exit).
There is no way to send a second turn to a live process:

- `codex exec --help`: "Initial instructions for the agent. If not provided as an argument (or
  if `-` is used), instructions are read from stdin. If stdin is piped and a prompt is also
  provided, stdin is appended as a `<stdin>` block." Stdin is therefore part of the *first*
  prompt, not a turn channel — and it is consumed before the turn starts. (The recorder leaves
  the child's stdin at `/dev/null`, which is why `Reading additional input from stdin...`
  appears on stderr with an empty `<stdin>` block.)
- There is no `--input-format stream-json` equivalent, no control-request channel, and no
  approval channel: `codex exec` has `--approve-for-me` (an automatic reviewer, no host
  involvement) and the two bypass flags Styr never passes, and nothing else.

A multi-turn session is therefore **one OS process per turn**: `codex exec … <prompt>` for the
first, `codex exec resume <thread_id> … <prompt>` for every later one.

## `codex exec resume`

`codex exec resume [SESSION_ID] [PROMPT]` **does** accept a prompt positionally (fixture 04),
and the resumed turn has the earlier turns' context — asked "What word did you reply with
before?", it answered `pong`, the word from fixture 01's turn.

`resume` is not simply `exec` with an extra id: **it accepts neither `-C/--cd` nor
`-s/--sandbox`** (compare `codex exec --help` with `codex exec resume --help`). Consequences
for Styr, both handled in `harness.go`:

- the working directory can only be set by the child process's own cwd (`cmd.Dir`), which Styr
  sets from `StartSpec.Cwd` for every turn anyway;
- the sandbox policy has to be re-stated as a config override, `-c sandbox_mode=<mode>`.
  `sandbox_mode` is a real config key (it appears in the binary's `ConfigProfile` field list),
  and the override is accepted by the argument parser — verified with a deliberately unknown
  thread id, which got as far as the thread lookup (`no rollout found for thread id …`) instead
  of a config error. Its *effect* on a resumed turn was not verified end to end; the session
  budget was spent. Without it the resumed turn would silently fall back to the CLI's default
  policy, which is exactly the failure mode the `investigate` profile must not have, so Styr
  passes it and re-asserts read-only on every resumed turn.

Note the value is passed unquoted (`sandbox_mode=read-only`): the `-c` value is parsed as TOML
and falls back to the raw string when that fails, which is what happens here.

`resume` filters candidate sessions by cwd unless `--all` is given; Styr always resumes from
the same cwd it started in, and always passes an explicit thread id, so this never applies.

## Exit codes

`0` on `turn.completed`; `1` on `turn.failed` and on a local failure that produces no stdout
(`05_resume_missing`). No other code was observed.

## Summary of the mapping Styr implements

| exec JSON | `harness.Event` |
| --- | --- |
| `thread.started` | `EventInit` — `SessionID` = `thread_id`, `Model` = what was passed as `-m`, `Harness` = `codex` |
| `turn.started` | — (nothing) |
| `item.*` / `agent_message` | `EventText` |
| `item.*` / `reasoning` | — (nothing; like a Claude `thinking` block) |
| `item.started` / `command_execution` | `EventToolUse` — `Name` `Bash`, `Input` `{"command":…}` |
| `item.completed` / `command_execution` | `EventToolResult` — `Content` = `aggregated_output`, `IsError` = `exit_code != 0` |
| `item.started` / `file_change` | `EventToolUse` — `Name` `Edit`, `Input` `{"file_path":…,"kind":…}`, one per change |
| `item.completed` / `file_change` | `EventToolResult` — the changed paths, `IsError` on `status: failed` |
| `item.*` / `error` | `EventRaw` with `Err` set (informational; does not end the turn) |
| `turn.completed` | `EventResult` — `NumTurns` 1, `CostUSD` 0, token counts, `StructuredOutput` |
| `turn.failed`, bare `error` | `EventResult` with `IsError` / `EventRaw` |
| anything else | `EventRaw` |
