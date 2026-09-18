#!/usr/bin/env bash
# Fake Claude Code CLI: replays a recorded fixture. Used by internal/harness/claude tests and
# `make dev-backend`. Ignores its argv except reading FAKE_CLAUDE_FIXTURE / FAKE_CLAUDE_DELAY
# from the environment. For each JSON line read on real stdin that looks like a "user" turn, it
# replays $FAKE_CLAUDE_FIXTURE from the top, skipping the fixture's ">>> " stdin-echo lines, and
# for a can_use_tool control_request it blocks (reading real stdin) until it sees a
# control_response before continuing. A "subtype":"interrupt" line on stdin gets a synthetic
# acknowledgement and result instead of touching the fixture.
set -euo pipefail
FIX="${FAKE_CLAUDE_FIXTURE:-$(dirname "$0")/../../internal/harness/claude/testdata/01_simple_text.jsonl}"
DELAY="${FAKE_CLAUDE_DELAY:-0.01}"

while IFS= read -r input; do
  case "$input" in
    *'"type":"user"'*)
      # Fixture lines come from fd 3, kept distinct from real stdin (fd 0), which the
      # can_use_tool wait below also reads from; conflating the two would let a fixture line
      # meant for stdout be consumed as the host's control_response answer, or vice versa.
      exec 3< "$FIX"
      while IFS= read -u 3 -r line; do
        [[ "$line" == '>>> '* ]] && continue
        printf '%s\n' "$line"
        if [[ "$DELAY" != "0" ]]; then
          sleep "$DELAY"
        fi
        if [[ "$line" == *'"type":"control_request"'* && "$line" == *'"can_use_tool"'* ]]; then
          while IFS= read -r ans; do
            [[ "$ans" == *'"type":"control_response"'* ]] && break
          done
        fi
      done
      exec 3<&-
      ;;
    *'"subtype":"interrupt"'*)
      printf '{"type":"control_response","response":{"subtype":"success","request_id":"x","response":{"still_queued":[]}}}\n'
      printf '{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0,"duration_ms":1,"result":"interrupted","session_id":"fake"}\n'
      ;;
  esac
done
