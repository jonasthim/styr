#!/usr/bin/env bash
# Fake Codex CLI: replays one recorded fixture per invocation. Used by internal/harness/codex
# tests (and available to `make dev-backend` the way fake-claude is). It models the real CLI's
# defining property — `codex exec` runs exactly one prompt and then exits — so every turn of a
# fake session is its own process, exactly as with the real binary.
#
# Argv shape it understands (everything else is ignored):
#   exec [resume <thread id>] --json [-C DIR] [--sandbox MODE] [-c KEY=VALUE]
#        [--skip-git-repo-check] [-m MODEL] [--output-schema FILE] [-o FILE] <prompt>
# The prompt is always the last argument.
#
# Fixture selection, in order: a "[fixture:NN]" marker anywhere in the prompt (e.g.
# "[fixture:04] continue") replays the NN_*.jsonl file in the fixture directory; otherwise
# $FAKE_CODEX_FIXTURE; otherwise 01_simple_text.jsonl. FAKE_CODEX_FIXTURE_DIR overrides the
# fixture directory used by both.
#
# The fixture's trailing ">>> exit N" line is not printed: it becomes the fake's exit status,
# so a fixture of a failed turn (03a, 05) makes the fake fail the same way. FAKE_CODEX_DELAY
# (default 0.01, "0" disables) sleeps between lines so tests can interrupt mid-turn.
#
# The structured output needs no special handling: the real CLI delivers a --output-schema
# answer as the turn's final agent_message on stdout (see internal/harness/codex/testdata/
# PROTOCOL.md), which is already in the fixture. --output-schema is still honoured to the
# extent that a missing schema file is reported as a failed turn, the way a real misconfigured
# run would be; and -o/--output-last-message, which Styr does not use, writes the last
# agent_message text to the named file.
#
# FAKE_CODEX_ARGV_LOG, when set, appends one tab-separated line per invocation (the full argv)
# to that file, which is how the process tests assert that a second turn really was a second
# process and really used `resume <thread id>`. A "[argvlog:PATH]" marker in the prompt names
# the same file and is what the e2e suite uses: Styr hands a child process only HOME, the
# PATH/TERM/LANG passthrough and its harness's own credential (internal/harness/codex/
# process.go), so an environment variable set on the *server* never reaches the CLI, while
# anything in the prompt does.
set -euo pipefail

DIR="${FAKE_CODEX_FIXTURE_DIR:-$(dirname "$0")/../../internal/harness/codex/testdata}"
FIX="${FAKE_CODEX_FIXTURE:-$DIR/01_simple_text.jsonl}"
DELAY="${FAKE_CODEX_DELAY:-0.01}"

schema=""
last_message_file=""
prev=""
for arg in "$@"; do
  case "$prev" in
    --output-schema) schema="$arg" ;;
    -o|--output-last-message) last_message_file="$arg" ;;
  esac
  prev="$arg"
  prompt="$arg"
done
prompt="${prompt:-}"

argv_log="${FAKE_CODEX_ARGV_LOG:-}"
if [[ "$prompt" =~ \[argvlog:([^]]+)\] ]]; then
  argv_log="${BASH_REMATCH[1]}"
fi
if [[ -n "$argv_log" ]]; then
  printf '%s\t' "$@" >> "$argv_log"
  printf '\n' >> "$argv_log"
fi

if [[ -n "$schema" && ! -r "$schema" ]]; then
  printf '{"type":"thread.started","thread_id":"00000000-0000-0000-0000-00000000fake"}\n'
  printf '{"type":"turn.started"}\n'
  printf '{"type":"turn.failed","error":{"message":"fake-codex: output schema %s is not readable"}}\n' "$schema"
  exit 1
fi

if [[ "$prompt" =~ \[fixture:([0-9a-z]+)\] ]]; then
  match="$(ls "$DIR"/"${BASH_REMATCH[1]}"_*.jsonl 2>/dev/null | head -n1 || true)"
  if [[ -n "$match" ]]; then
    FIX="$match"
  fi
fi

# The recorded stderr, when present, is replayed too: a CLI failure that never reaches the API
# reports itself only there (fixture 05), and the runner has to surface that line.
if [[ -r "$FIX.stderr" ]]; then
  cat "$FIX.stderr" >&2
fi

code=0
last_text=""
while IFS= read -r line; do
  if [[ "$line" == '>>> exit '* ]]; then
    code="${line#>>> exit }"
    continue
  fi
  [[ -z "$line" ]] && continue
  printf '%s\n' "$line"
  if [[ "$line" == *'"type":"agent_message"'* ]]; then
    last_text="$line"
  fi
  if [[ "$DELAY" != "0" ]]; then
    sleep "$DELAY"
  fi
done < "$FIX"

if [[ -n "$last_message_file" ]]; then
  printf '%s\n' "$last_text" > "$last_message_file"
fi
exit "$code"
