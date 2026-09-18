# Observed stream-json protocol — Claude Code CLI 2.1.276

Recorded 2026-09-18 by `hack/recorder/main.go` against the real, logged-in workstation CLI
(Max subscription, no `ANTHROPIC_API_KEY`). Command line used by the recorder:

```
claude -p --verbose --input-format stream-json --output-format stream-json \
  --include-partial-messages --replay-user-messages --session-id SID --name styr-fixture \
  --permission-mode default|manual --permission-prompt-tool stdio --max-turns 6
```

Note: `--verbose` had to be added to the recorder beyond what the plan drafted — the CLI
refuses to start with `--print` (`-p`) and `--output-format=stream-json` unless `--verbose` is
also given (`Error: When using --print, --output-format=stream-json requires --verbose`).

**Recording was stopped early per the card's fixture-03 rule.** Only three of the five
sessions were run (of the 6-session budget: fixtures 01, 02, and two attempts at 03 = 4
sessions used). Fixtures `04_multi_turn.jsonl` and `05_interrupt.jsonl` were **not** recorded.
See "Fixture 03: no control_request observed" below for why.

## Message types observed

### `system`, subtype `init`

One real line (fixture 01, line 12; wrapped here only for legibility — the file itself has it
as a single line):

```json
{"type":"system","subtype":"init","cwd":"/tmp/styr-fixture-ws","session_id":"8d45afe0-771b-4803-8379-491dd9455365","tools":["Task","AskUserQuestion","Bash","CronCreate","CronDelete","CronList","DesignSync","Edit","EnterPlanMode","EnterWorktree","ExitPlanMode","ExitWorktree","ListAgents","LSP","Monitor","NotebookEdit","PushNotification","Read","RemoteTrigger","ReportFindings","ScheduleWakeup","SendMessage","Skill","TaskOutput","TaskStop","ToolSearch","WebFetch","WebSearch","Workflow","Write","mcp__plugin_cloudflare_cloudflare-docs__migrate_pages_to_workers_guide","mcp__plugin_cloudflare_cloudflare-docs__search_cloudflare_documentation","mcp__plugin_playwright_playwright__browser_click","mcp__plugin_playwright_playwright__browser_close","mcp__plugin_playwright_playwright__browser_console_messages","mcp__plugin_playwright_playwright__browser_drag","mcp__plugin_playwright_playwright__browser_drop","mcp__plugin_playwright_playwright__browser_evaluate","mcp__plugin_playwright_playwright__browser_file_upload","mcp__plugin_playwright_playwright__browser_fill_form","mcp__plugin_playwright_playwright__browser_find","mcp__plugin_playwright_playwright__browser_handle_dialog","mcp__plugin_playwright_playwright__browser_hover","mcp__plugin_playwright_playwright__browser_navigate","mcp__plugin_playwright_playwright__browser_navigate_back","mcp__plugin_playwright_playwright__browser_network_request","mcp__plugin_playwright_playwright__browser_network_requests","mcp__plugin_playwright_playwright__browser_press_key","mcp__plugin_playwright_playwright__browser_resize","mcp__plugin_playwright_playwright__browser_run_code_unsafe","mcp__plugin_playwright_playwright__browser_select_option","mcp__plugin_playwright_playwright__browser_snapshot","mcp__plugin_playwright_playwright__browser_tabs","mcp__plugin_playwright_playwright__browser_take_screenshot","mcp__plugin_playwright_playwright__browser_type","mcp__plugin_playwright_playwright__browser_wait_for","mcp__plugin_playwright_playwright__browser_webmcp_call","mcp__plugin_playwright_playwright__browser_webmcp_list"],"mcp_servers":[{"name":"plugin:context7:context7","status":"pending","source":"plugin"},{"name":"plugin:github:github","status":"pending","source":"plugin"},{"name":"plugin:playwright:playwright","status":"connected","source":"plugin"},{"name":"plugin:terraform:terraform","status":"failed","source":"plugin"},{"name":"plugin:cloudflare:cloudflare-api","status":"needs-auth","source":"plugin"},{"name":"plugin:cloudflare:cloudflare-docs","status":"connected","source":"plugin"},{"name":"plugin:cloudflare:cloudflare-builds","status":"needs-auth","source":"plugin"},{"name":"plugin:cloudflare:cloudflare-observability","status":"needs-auth","source":"plugin"},{"name":"claude.ai Claude Docs","status":"pending","source":"claudeai"},{"name":"claude.ai Spotify","status":"pending","source":"claudeai"},{"name":"claude.ai Cloudflare Developer Platform","status":"needs-auth","source":"claudeai"},{"name":"claude.ai Home Assistant","status":"needs-auth","source":"claudeai"},{"name":"claude.ai Google Calendar","status":"needs-auth","source":"claudeai"},{"name":"claude.ai Google Drive","status":"needs-auth","source":"claudeai"},{"name":"claude.ai Gmail","status":"pending","source":"claudeai"}],"model":"claude-fable-5-1","permissionMode":"default","slash_commands":["archify","deep-research","code-review:code-review","feature-dev:feature-dev","claude-md-management:revise-claude-md","ralph-loop:cancel-ralph","ralph-loop:help","ralph-loop:ralph-loop","commit-commands:clean_gone","commit-commands:commit-push-pr","commit-commands:commit","pr-review-toolkit:review-pr","cloudflare:build-agent","cloudflare:build-mcp","codex:adversarial-review","codex:cancel","codex:rescue","codex:result","codex:review","codex:setup","codex:status","codex:transfer","frontend-design:frontend-design","superpowers:brainstorming","superpowers:dispatching-parallel-agents","superpowers:executing-plans","superpowers:finishing-a-development-branch","superpowers:receiving-code-review","superpowers:requesting-code-review","superpowers:subagent-driven-development","superpowers:systematic-debugging","superpowers:test-driven-development","superpowers:using-git-worktrees","superpowers:using-superpowers","superpowers:verification-before-completion","superpowers:writing-plans","superpowers:writing-skills","claude-md-management:claude-md-improver","claude-code-setup:claude-automation-recommender","cloudflare:agents-sdk","cloudflare:building-ai-agent-on-cloudflare","cloudflare:building-mcp-server-on-cloudflare","cloudflare:cloudflare","cloudflare:durable-objects","cloudflare:sandbox-sdk","cloudflare:web-perf","cloudflare:workers-best-practices","cloudflare:wrangler","claude-obsidian:autoresearch","claude-obsidian:canvas","claude-obsidian:defuddle","claude-obsidian:obsidian-bases","claude-obsidian:obsidian-markdown","claude-obsidian:save","claude-obsidian:think","claude-obsidian:wiki","claude-obsidian:wiki-cli","claude-obsidian:wiki-fold","claude-obsidian:wiki-ingest","claude-obsidian:wiki-lint","claude-obsidian:wiki-mode","claude-obsidian:wiki-query","claude-obsidian:wiki-retrieve","design","design-sync","dataviz","update-config","verify","debug","code-review","simplify","batch","fewer-permission-prompts","doctor","loop","schedule","claude-api","workflow-authoring","run","run-skill-generator","advisor","agents","auto-mode-setup","autocompact","clear","color","compact","config","output-style","context","effort","fast","heapdump","init","mcp","import","model","__remote-workflow","workflow-launch-exec","reload-plugins","reload-skills","rename","ultrareview","security-review","usage-credits","extra-usage","usage","insights","recap","skill-doctor","goal","design-consent","design-revoke","list-agents","team-onboarding","mcp__plugin_cloudflare_cloudflare-docs__workers-prompt-full"],"terminal_slash_commands":["doctor","color","reload-plugins"],"apiKeySource":"none","claude_code_version":"2.1.276","output_style":"default","agents":["claude","claude-obsidian:verifier","claude-obsidian:wiki-ingest","claude-obsidian:wiki-lint","code-simplifier:code-simplifier","codex:codex-rescue","Explore","feature-dev:code-architect","feature-dev:code-explorer","feature-dev:code-reviewer","general-purpose","Plan","pr-review-toolkit:code-reviewer","pr-review-toolkit:code-simplifier","pr-review-toolkit:comment-analyzer","pr-review-toolkit:pr-test-analyzer","pr-review-toolkit:silent-failure-hunter","pr-review-toolkit:type-design-analyzer","statusline-setup"],"skills":["archify","deep-research","codex:adversarial-review","codex:cancel","codex:result","codex:review","codex:status","codex:transfer","frontend-design:frontend-design","superpowers:brainstorming","superpowers:dispatching-parallel-agents","superpowers:executing-plans","superpowers:finishing-a-development-branch","superpowers:receiving-code-review","superpowers:requesting-code-review","superpowers:subagent-driven-development","superpowers:systematic-debugging","superpowers:test-driven-development","superpowers:using-git-worktrees","superpowers:using-superpowers","superpowers:verification-before-completion","superpowers:writing-plans","superpowers:writing-skills","claude-md-management:claude-md-improver","claude-code-setup:claude-automation-recommender","cloudflare:agents-sdk","cloudflare:building-ai-agent-on-cloudflare","cloudflare:building-mcp-server-on-cloudflare","cloudflare:cloudflare","cloudflare:durable-objects","cloudflare:sandbox-sdk","cloudflare:web-perf","cloudflare:workers-best-practices","cloudflare:wrangler","claude-obsidian:autoresearch","claude-obsidian:canvas","claude-obsidian:defuddle","claude-obsidian:obsidian-bases","claude-obsidian:obsidian-markdown","claude-obsidian:save","claude-obsidian:think","claude-obsidian:wiki","claude-obsidian:wiki-cli","claude-obsidian:wiki-fold","claude-obsidian:wiki-ingest","claude-obsidian:wiki-lint","claude-obsidian:wiki-mode","claude-obsidian:wiki-query","claude-obsidian:wiki-retrieve","design","design-sync","dataviz","update-config","verify","debug","code-review","simplify","batch","fewer-permission-prompts","doctor","loop","schedule","claude-api","workflow-authoring","run","run-skill-generator"],"plugins":[{"name":"frontend-design","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/frontend-design/1aa8f02ec832","source":"frontend-design@claude-plugins-official"},{"name":"superpowers","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/superpowers/6.3.0","source":"superpowers@claude-plugins-official","version":"6.3.0"},{"name":"context7","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/context7/1aa8f02ec832","source":"context7@claude-plugins-official"},{"name":"code-review","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/code-review/1aa8f02ec832","source":"code-review@claude-plugins-official"},{"name":"code-simplifier","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/code-simplifier/1.0.0","source":"code-simplifier@claude-plugins-official","version":"1.0.0"},{"name":"github","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/github/1aa8f02ec832","source":"github@claude-plugins-official"},{"name":"playwright","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/playwright/1aa8f02ec832","source":"playwright@claude-plugins-official"},{"name":"feature-dev","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/feature-dev/1aa8f02ec832","source":"feature-dev@claude-plugins-official"},{"name":"claude-md-management","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/claude-md-management/1.0.0","source":"claude-md-management@claude-plugins-official","version":"1.0.0"},{"name":"ralph-loop","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/ralph-loop/1.0.0","source":"ralph-loop@claude-plugins-official","version":"1.0.0"},{"name":"typescript-lsp","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/typescript-lsp/1.0.0","source":"typescript-lsp@claude-plugins-official","version":"1.0.0"},{"name":"security-guidance","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/security-guidance/2.0.8","source":"security-guidance@claude-plugins-official","version":"2.0.8"},{"name":"commit-commands","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/commit-commands/1aa8f02ec832","source":"commit-commands@claude-plugins-official"},{"name":"claude-code-setup","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/claude-code-setup/1.0.0","source":"claude-code-setup@claude-plugins-official","version":"1.0.0"},{"name":"pr-review-toolkit","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/pr-review-toolkit/1aa8f02ec832","source":"pr-review-toolkit@claude-plugins-official"},{"name":"terraform","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/terraform/1aa8f02ec832","source":"terraform@claude-plugins-official"},{"name":"cloudflare","path":"/home/thim/.claude/plugins/cache/claude-plugins-official/cloudflare/1.0.0","source":"cloudflare@claude-plugins-official","version":"1.0.0"},{"name":"claude-obsidian","path":"/home/thim/Repositories/claude-obsidian","source":"claude-obsidian@agricidaniel-claude-obsidian","version":"2.1.0"},{"name":"codex","path":"/home/thim/.claude/plugins/cache/openai-codex/codex/1.0.6","source":"codex@openai-codex","version":"1.0.6"}],"capabilities":["interrupt_receipt_v1","interrupt_cancel_queued_v1","msg_lifecycle_v1"],"analytics_disabled":false,"product_feedback_disabled":false,"uuid":"d89f8e1b-4f3c-4ede-9215-de61fc3564ed","memory_paths":{"auto":"/home/thim/.claude/projects/-tmp-styr-fixture-ws/memory/"},"messaging_socket_path":"/run/user/1000/cc-socks/1194704.sock","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required"}
```

Fields Styr uses: `session_id` (top-level string), `model` (top-level string, e.g.
`"claude-fable-5-1"`), `tools` (top-level array of tool name strings — this is the CLI's own
full tool list, not a Styr-specific allowlist), `cwd`, `permissionMode`, `claude_code_version`.
`session_id` and `model` are both top-level fields on the `init` envelope itself, not nested
under `message`.

Other `system` subtypes observed that are **not** in the plan's required list and that Styr's
codec should treat as `EventRaw` (or ignore): `hook_started`, `hook_response` (fired because
the recorder does not isolate `HOME`/`XDG` for the child process, so the operator's own
`SessionStart` hooks ran), `status`, `commands_changed`. A production Styr deployment gives
each user an isolated `HOME` (per the design doc), so these should not appear in real sessions
unless the user has their own hooks configured under that isolated `HOME`.

### `assistant`

Each content block arrives as its **own** top-level `assistant` envelope — the CLI does not
batch multiple content blocks (thinking, tool_use, text) into one `assistant` line's
`message.content` array in practice; each line's `message.content` has exactly one element,
but repeats the same `message.id` and `request_id` across the lines that belong to one logical
assistant turn. Styr's codec must iterate `message.content[]` per the plan (correct per-spec,
since the array *could* hold more than one block), but should not assume all blocks of a turn
arrive on one line.

Text block (fixture 01, line 21):

```json
{"type":"assistant","message":{"model":"claude-fable-5-1","id":"msg_011CfB7Zk85MsAaxKHE18LSk","type":"message","role":"assistant","content":[{"type":"text","text":"pong"}],"container":null,"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":19826,"cache_read_input_tokens":11590,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":19826},"output_tokens":1,"service_tier":"standard","inference_geo":"not_available"},"input_transformations":[],"diagnostics":null,"context_management":null},"parent_tool_use_id":null,"session_id":"8d45afe0-771b-4803-8379-491dd9455365","uuid":"c78d6e0d-2ff2-4bac-b4b7-dde90a0885e3","timestamp":"2026-09-18T15:07:28.916Z","request_id":"req_011CfB7ZjY6w4K77zth5VFDr"}
```

Tool-use block, complete with `input` (fixture 02, line 27):

```json
{"type":"assistant","message":{"model":"claude-fable-5-1","id":"msg_011CfB7feUAjW6Kx5dfhRGXE","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01JGXzWaNtEkrsHrMVyuvqdP","name":"Read","input":{"file_path":"/tmp/styr-fixture-ws/note.txt"},"caller":{"type":"direct"}}],"container":null,"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":18090,"cache_read_input_tokens":15119,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":18090},"output_tokens":2,"service_tier":"standard","inference_geo":"not_available"},"input_transformations":[],"diagnostics":null,"context_management":null},"parent_tool_use_id":null,"session_id":"180be86c-2552-4676-a118-3ebfe8122940","uuid":"d1fa63de-1c3e-4586-8574-2a2114394c27","timestamp":"2026-09-18T15:08:49.404Z","request_id":"req_011CfB7fdsCsDHxgK1qqBa7R","wire_tool_inputs":{"toolu_01JGXzWaNtEkrsHrMVyuvqdP":{"file_path":"/tmp/styr-fixture-ws/note.txt"}}}
```

`session_id` and `parent_tool_use_id` are top-level fields (sibling of `message`), not inside
`message`. `message.content[].id` is the tool_use id later echoed as `tool_use_id` in the
paired `user`/`tool_result` message.

A third content block type was observed that the plan's `PROTOCOL.md` outline does not
mention: `"type":"thinking"` (fixture 02, line 19), with fields `thinking` (string, empty in
this run) and `signature` (long opaque string). Styr's codec should decode it as `EventRaw` (or
a new `EventThinking` if Task 4 wants to surface it) rather than fail decoding.

### `user` (tool_result)

Fixture 02, line 29 — content is an **array** of blocks, `content[0].content` is a **string**
that is often pretokenized as a numbered-lines view (Read tool):

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01JGXzWaNtEkrsHrMVyuvqdP","type":"tool_result","content":"1\thello\n2\t"}]},"parent_tool_use_id":null,"session_id":"180be86c-2552-4676-a118-3ebfe8122940","uuid":"a1a3f268-487b-4a93-96b1-8b64ba24b017","timestamp":"2026-09-18T15:08:49.412Z","tool_use_result":{"type":"text","file":{"filePath":"/tmp/styr-fixture-ws/note.txt","content":"hello\n","numLines":2,"startLine":1,"totalLines":2}}}
```

`message.content[0].tool_use_id` pairs with the `assistant` tool_use block's `message.content[0].id`.
`message.content[0].content` was a plain JSON string in both cases observed (`"1\thello\n2\t"`
for the Read tool result, `"styr-ok"` implied by the final `result.result` for the Bash case —
fixture 03 did not surface an intermediate `tool_result` user line because the model's Bash
tool_use in that run produced `is_error` false too; see raw fixture for the full line). No
array-of-text-block form of `content` was observed in these three fixtures — the plan's
`DecodeLine` should still call `flattenContent` defensively (string or array) as drafted.

A second `user` shape was also observed: the **echo of our own stdin turn**, re-emitted by the
CLI because of `--replay-user-messages` (fixture 01, line 16 shown below), and our own stdin
line that produced it (fixture 01, line 1, the `>>> `-prefixed line the recorder writes):

```json
>>> {"message":{"content":"Reply with exactly the word pong and nothing else.","role":"user"},"parent_tool_use_id":null,"session_id":"8d45afe0-771b-4803-8379-491dd9455365","type":"user"}
```

echoed back on stdout as:

```json
(see message text above; ">>> " prefix removed, plus session-assigned "uuid", "timestamp", "isReplay":true)
```

Styr's `DecodeLine` must special-case this: a `user` message whose `message.content` is a
**plain string** (not an array of `tool_result` blocks) is the echo of our own turn and
produces **no** event (the plan's draft codec already does this by falling through to
`return nil`).

### `stream_event`

`event.type` values observed: `message_start`, `content_block_start`, `content_block_delta`,
`content_block_stop`, `message_delta`, `message_stop`. Only `content_block_delta` with
`delta.type == "text_delta"` carries partial text (fixture 01, line 19):

```json
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"p"}},"session_id":"8d45afe0-771b-4803-8379-491dd9455365","parent_tool_use_id":null,"uuid":"79775b55-73d3-4175-980d-291d6328ea89"}
```

`event.delta.text` is the incremental text chunk. `stream_event` envelopes carry `session_id`
and `parent_tool_use_id` at the top level (sibling of `event`), same shape as `assistant`.

### `result`

Fixture 01, line 26 (all fields, single line):

```json
{"duration_api_ms":2218,"stop_reason":"end_turn","session_id":"8d45afe0-771b-4803-8379-491dd9455365","total_cost_usd":0.3996375,"usage":{"input_tokens":2,"cache_creation_input_tokens":19826,"cache_read_input_tokens":11590,"output_tokens":4,"output_tokens_details":{"thinking_tokens":0},"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":19826,"ephemeral_5m_input_tokens":0},"inference_geo":"not_available","iterations":[{"input_tokens":2,"output_tokens":4,"cache_read_input_tokens":11590,"cache_creation_input_tokens":19826,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":19826},"type":"message","model":null}],"speed":"standard"},"modelUsage":{"claude-fable-5-1":{"inputTokens":2,"outputTokens":4,"cacheReadInputTokens":11590,"cacheCreationInputTokens":19826,"webSearchRequests":0,"costUSD":0.3996375,"contextWindow":1000000,"maxOutputTokens":64000,"thinkingTokens":0,"canonicalModel":"claude-fable-5-1","provider":"firstParty","costBasis":"list"}},"permission_denials":[],"terminal_reason":"completed","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subagent_stats":{"spawned":0,"requested":{"background":0,"foreground":0,"unset":0},"started_in_background":0,"max_depth":0,"spawned_by_subagents":0,"completed":0,"failed":0,"killed":{"parent":0,"user":0,"system":0},"refused":{"depth_limit":0,"concurrency_limit":0,"budget":0},"by_type":{}},"is_error":false,"num_turns":1,"subtype":"success","api_error_status":null,"result":"pong","ttft_ms":2303,"type":"result","duration_ms":2460,"uuid":"c9ec0819-6b28-4029-8343-0ac3fe0b7e38","ttft_stream_ms":1420,"time_to_request_ms":106,"first_content_frame_ms":1420,"queued_turn_count":0,"result_index":0}
```

Fields Styr uses, all top-level on the `result` envelope: `subtype` (`"success"` observed),
`is_error` (bool), `num_turns` (int), `total_cost_usd` (float), `usage.input_tokens`,
`usage.output_tokens` (nested under `usage`), `duration_ms` (int), `result` (string — final
assistant text). Many additional fields are present (`modelUsage`, `subagent_stats`,
`permission_denials`, `terminal_reason`, `ttft_ms`, ...) that the plan's `Result` struct does
not model; the codec should ignore unknown fields (Go's `encoding/json` does this by default)
rather than fail.

`permission_denials` is present and was an empty array (`[]`) in all three fixtures, including
fixture 03 where a Bash tool_use ran. This is the same field referenced in the "Fixture 03"
section below.

### `control_request` / `control_response`

**Not observed in any of the three recorded sessions**, including the Bash-tool fixture run
twice (once `--permission-mode default`, once `--permission-mode manual`). See below.

### stdin lines (`>>> ` prefix in the fixture files)

Two shapes were sent by the recorder and are visible with the `>>> ` prefix: the initial user
turn (shown above) and, for fixture 03's would-be interrupt/permission handling, nothing extra
was needed since no `control_request` ever arrived to answer. The `EncodeUser` shape used by
the recorder is:

```json
{"type":"user","message":{"role":"user","content":"TEXT"},"parent_tool_use_id":null,"session_id":"SID"}
```

which matches the plan's documented wire shape exactly.

### Process exit behaviour

In all three sessions, after the recorder called `stdin.Close()` (once `sendNext()` returned
`false` following the final `result` line), the `claude` process exited on its own and
`cmd.Wait()` returned without needing to be killed. No hang was observed in any of the three
sessions actually run.

## Fixture 03: no `control_request` observed

`03_permission_bash.jsonl` was recorded twice, both attempts asking the model to run
`echo styr-ok` via the Bash tool:

1. `--permission-mode default` — the CLI ran the `Bash` tool_use immediately; the `result`
   line's `permission_denials` field was `[]` and no `control_request` line of any subtype
   appeared anywhere in the output.
2. `--permission-mode manual` (the card's specified retry) — identical outcome: `Bash` ran
   without any `control_request`/prompt, `permission_denials: []`.

Full multiset of `type`/`subtype` values that appeared across both fixture-03 attempts (from
the final, kept `manual`-mode recording — the plan's card asks for exactly this list):

```
system/init            x1
system/hook_started    x5
system/hook_response   x5
system/status          x2
system/commands_changed (not present in this run; commands_changed only appeared in fixture 01/02)
assistant               x2   (one "text" content block, one "tool_use" content block)
stream_event            x19  (message_start, content_block_start, content_block_delta,
                               content_block_stop, message_delta, message_stop)
user                     x1   (echo of our stdin turn only — no tool_result line was emitted
                               for the Bash call either; the model's final assistant text was
                               derived without an intervening tool_result envelope appearing on
                               stdout in this run)
result                   x1   (subtype "success", is_error false, permission_denials: [])
```

No line anywhere in either attempt has `"type":"control_request"` or
`"type":"control_response"`.

**Most likely cause:** the recorder does not isolate `HOME` for the child `claude` process (the
plan's recorder code as given does not set `cmd.Env`), so the spawned CLI inherits the
operator's real `~/.claude` user settings — which, on this workstation, evidently already
allow-list the `Bash` tool (or this specific command) well enough that `--permission-prompt-tool
stdio` never needs to ask, regardless of `--permission-mode`. This is consistent with
`permission_denials: []` (no denial *or* request was ever evaluated on the wire) and with
fixture 02's `Read` tool also running with no prompt (expected — `Read` is not normally
permission-gated). It is also possible that `--permission-mode manual` behaves the same as
`default` for tools already covered by an allow rule, i.e. explicit allow rules short-circuit
prompting entirely and no permission mode re-enables it.

Per the card's instruction, no alternative flags (e.g. clearing/overriding the inherited
settings, or `--dangerously-skip-permissions`, which is forbidden anyway) were attempted beyond
the specified `-mode manual` retry. **This blocks fixture 03 and, in turn, `04_multi_turn.jsonl`
and `05_interrupt.jsonl` were also not recorded** so as not to spend the remaining session
budget before the human decides the fallback: either (a) re-run the recorder with an isolated
`HOME`/settings directory that has no allow rules (risk: this workstation's `claude` login
state also lives under `HOME`, so isolating it may require a copy of just the credential store
into a scratch `HOME`, which this task was not authorized to inspect/construct), or (b) accept
that Styr's runtime `HOME` isolation (per the design doc, `internal/harness` gives each spawned
process its own `HOME` under the data dir with no operator-authored `settings.json` allow
rules) already guarantees `control_request` fires as documented, and that this spike's failure
to observe it is an artifact of running on the operator's own polluted `HOME`, not of the
protocol itself — in which case Task 4 can still code the `control_request`/`control_response`
shapes from the plan's card (they match the Agent SDK's own documented `canUseTool` wire
format) without a fixture, and this task should be re-run in a follow-up spike once `HOME`
isolation is available to produce a real fixture.
