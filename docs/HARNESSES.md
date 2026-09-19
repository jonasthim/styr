# Harnesses

A *harness* is Styr's adapter for one agentic CLI. Everything above it — the session service,
the transcript, approvals, review, runs and pipelines — is written against the interface in
`internal/harness/types.go` and knows nothing about any particular CLI's wire format.

Styr v1.0 ships two: **Claude Code** (`claude`) and the **OpenAI Codex CLI** (`codex`).

## Choosing one

A session's harness is a column on its own row, chosen in this order:

1. `harness` on `POST /api/v1/sessions` (the new-session dialog's harness select);
2. the profile's `harness` (Settings → Profiles);
3. `claude`.

`GET /api/v1/status` reports `harnesses: [{kind, available, version, bin}]`, probed once at
startup by running `<bin> --version`. A harness whose binary could not be run is listed with
`available: false` rather than hidden, so the UI can explain a choice it must not offer instead
of silently dropping it. The binaries are configured as `claude_bin` and `codex_bin`
(`STYR_CLAUDE_BIN`, `STYR_CODEX_BIN`).

## What each harness supports

| | Claude Code | Codex |
|---|---|---|
| Per-command approvals | yes (`--permission-prompt-tool stdio`) | **no** — sandbox policy only |
| Streaming partials | yes (`--include-partial-messages`) | no — whole messages only |
| Model switch mid-session | yes (restart + `--resume`, `--model`) | yes (restart, `-m`) |
| Reasoning effort | yes (`--effort`) | no |
| Structured output | yes (`--json-schema`, schema inline) | yes (`--output-schema`, schema **file**) |
| Cost reporting | yes (`total_cost_usd` on the result) | no — token counts only |
| Multi-turn in one process | yes (stdin `stream-json`) | **no** — one process per turn |
| Worktrees | yes, Styr-managed (also has its own `--worktree`) | yes, Styr-managed |
| Slash commands | yes (reported on init) | no |

### Approvals

This is the difference that shows through to the UI. Claude Code asks the host before each
tool use it is not already allowed to make, and Styr turns that into an approval in the inbox.

The Codex CLI has no equivalent: its permission model is the **sandbox policy** chosen when the
process starts, and it never asks the host anything. So:

- `internal/harness/codex`'s `Decide` returns `harness.ErrUnsupported`, and
  `sessions.Service.Decide` refuses a Codex session up front with `409 conflict`,
  "this harness has no approvals";
- Codex sessions never produce approvals, so nothing lands in the inbox for them;
- the session view hides the approval affordances for a Codex session, and the new-session
  dialog says so when Codex is chosen.

Styr maps a profile to a sandbox policy instead (`codex.SandboxMode`):

- `plan` and `dontAsk` → `--sandbox read-only`;
- a profile that disallows both `Edit` and `Write` → `--sandbox read-only`. This is how the
  builtin **investigate** profile expresses "look, do not touch";
- everything else → `--sandbox workspace-write`, i.e. writes confined to the session's own
  working directory (its worktree, when the workspace has worktrees enabled).

Neither of the CLI's bypass flags is ever emitted, by either harness. `make check` greps the
whole repository for them.

### Structured output and the `additionalProperties` caveat

Both harnesses can constrain a final turn to a JSON schema, which is what an unattended run's
report is built from. They differ in two ways that matter when a template is reused across
harnesses:

- Claude Code takes the schema **inline** (`--json-schema`); Codex takes a **file**
  (`--output-schema`), so `internal/harness/codex` writes the schema to a temp file per session
  and passes its path.
- The OpenAI API requires `"additionalProperties": false` on **every object** in the schema.
  A schema that works fine with the Claude harness is rejected by the Codex one, and the
  rejection arrives as a *failed turn* (exit 1, `turn.failed`) rather than as a start-up error.
  See `internal/harness/codex/testdata/PROTOCOL.md` and fixture `03a_schema_rejected.jsonl`.

### Cost reporting

Claude Code reports `total_cost_usd` on every result, which is what the sessions list, the
stats page and a run's cost badge show. The Codex CLI reports token counts only
(`turn.completed.usage`), so a Codex session's `cost_usd` stays 0 while its token counters are
accurate. Nothing extrapolates a price from tokens: a guessed number in a cost column is worse
than an honest zero.

### One process per turn

`codex exec` takes exactly one prompt and exits when the turn ends; piped stdin is folded into
the *first* prompt rather than being a channel for later turns. A Codex session is therefore a
sequence of short-lived processes behind one long-lived event stream: the first turn is
`codex exec`, every later turn in the same process object is `codex exec resume <thread id>`.

Two consequences:

- `Interrupt` kills the running turn's process; the turn ends with an `interrupted` result.
- A Styr *resume* — reopening a closed session, or a model switch — builds a **new** process
  object, which has not yet learned a thread id, so the CLI starts a fresh thread. Styr's own
  transcript is continuous either way (it is Styr's event log, not the CLI's), but the Codex
  side loses its conversation context across a restart. Carrying the thread id across restarts
  is a candidate for a later card.

## Credentials

Each harness has its own credential, sealed with the server's secretbox
(`internal/crypto`) and handed to the child process in its environment and nowhere else. A
child gets exactly the one variable its harness needs — a Codex child never sees a Claude
token, and the reverse.

| | Claude Code | Codex |
|---|---|---|
| Per user | `PUT /api/v1/me/claude-token` | `PUT /api/v1/me/codex-key` |
| Service-wide (unattended runs) | `PUT /api/v1/settings/service-token` | `PUT /api/v1/settings/codex-key` |
| Table | `claude_tokens` | `codex_credentials` |
| Child env | `CLAUDE_CODE_OAUTH_TOKEN` | `OPENAI_API_KEY` |
| Label stored | `…` + last 6 | `…` + last 4 |

**v1.0 scope:** the Codex credential is an **OpenAI API key** the user pastes in, not a
`codex login` under their Styr HOME. The v1.0 plan floated spiking headless
`codex login --device-auth`; that spike was deliberately not attempted for this release, so
there is no "Sign in to Codex" flow — a device-auth card can add one later without changing
anything above this line, since the credential resolution is already per harness.

Both keys are verified before anything is stored, by the same `Verifier` pattern
(`internal/harness/{claude,codex}/verify.go`): one minimal CLI turn with the credential in the
environment, a fresh empty HOME, a 60 s timeout, and the credential redacted out of whatever
the CLI printed before it reaches a log or an error message. The Codex verifier runs
`codex exec --skip-git-repo-check --sandbox read-only -C <tmp> "Reply with pong."` in an empty
temp directory, so a verification can neither read nor change anything of the operator's. In
dev mode (`env: dev`) both verifiers are replaced by prefix checks (`sk-ant-`, `sk-`) that
never spawn a process.

An unattended session (a webhook, schedule or pipeline run, which has no owner) resolves the
**service-wide** credential for its harness; an owned session resolves the owner's.

## Adding a harness

1. **Record fixtures first.** Drive the real CLI once per behaviour you need (a plain text
   turn, a tool call, a failure, a resume) and commit the raw output under
   `internal/harness/<name>/testdata/`, with a `PROTOCOL.md` that writes down what the stream
   actually looks like — ids, event ordering, where the structured answer lands, how resume
   works, whether one process can take a second turn. Every later decision argues from that
   file rather than from memory.
2. **Add the kind.** `harness.Kind` constant in `internal/harness/types.go`, and add it to
   `ValidKind`.
3. **Implement `harness.Harness` and `harness.Process`** in `internal/harness/<name>/`: a codec
   that maps the CLI's wire format onto `harness.Event`, and a process that owns the CLI's
   lifecycle. Return `harness.ErrUnsupported` from anything the CLI genuinely cannot do, rather
   than faking it — callers degrade on `errors.Is` and hide the affordance.
   Every codec must set `Init.Harness`: the init message is the authority on what actually
   answered, and `sessions` stores it back on the session row.
4. **Add a shell fake** under `testdata/fake-<name>/` that replays the fixtures, and test the
   process against it. Tests never run the real CLI.
5. **Add a verifier** if the credential is not already one Styr stores.
6. **Register it** in `cmd/styr/wire.go` (`registry.Register(...)`) and give it a `<name>_bin`
   config key with a `STYR_<NAME>_BIN` override.
7. **Resolve its credential** in `sessions.Service.credentialEnv`, and add the store, the
   routes and the profile card if it needs a new one.
8. **Surface it**: the `harness` enums in `docs/openapi.yaml`, the harness selects in the
   new-session dialog and the profiles table, the mock handlers, and a row in the table at the
   top of this file.
