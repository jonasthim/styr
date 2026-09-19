// Shared mutable session seed state, split out of handlers.ts so
// triggersHandlers.ts (T33) can seed webhook-origin sessions for its three
// seeded runs and read them back for GET /runs/{id}'s embedded session
// summary, without a circular import between the two handler modules.
// handlers.ts still owns every /api/v1/sessions* route; this module only
// owns the backing array and the ids other seed data needs to reference.
import type { Effort, ModelOption, Session } from '../api/types'
import { MOCK_EVENT_SESSION_ID } from './fakeEventSource'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

export const DEV_USER_ID = '00000000-0000-4000-8000-000000000001'

// Fixed id for the Task 20 seeded session (fixture 02: a Read tool call then
// "hello"). Hardcoded rather than exported for e2e: e2e specs run outside
// Vite, so they can't import a module that pulls in a `?raw` fixture import.
export const TOOL_FIXTURE_SESSION_ID = '00000000-0000-4000-8000-000000000005'

// --- T38: model, effort and slash commands (additive) ----------------------
// GET /status's static choices, mirroring internal/api/status_handlers.go and
// internal/harness/claude/builtins.go, plus the command list the CLI reports
// on init (a realistic mix: custom commands, a plugin skill, a headless-safe
// built-in and two hidden ones, so a spec can prove the filter works).
export const MOCK_MODELS: ModelOption[] = [
  { alias: 'fable', label: 'Fable 5.1' },
  { alias: 'opus', label: 'Opus 5' },
  { alias: 'sonnet', label: 'Sonnet 5' },
  { alias: 'haiku', label: 'Haiku 4.5' },
]

export const MOCK_EFFORTS: Exclude<Effort, ''>[] = ['low', 'medium', 'high', 'xhigh', 'max']

// The harness roster GET /status reports (internal/api/deps.go's HarnessInfo).
// Both are available in the mock: a mock-mode spec must be able to pick either
// one in the new-session dialog.
export const MOCK_HARNESSES = [
  { kind: 'claude' as const, available: true, version: '2.1.276', bin: 'claude' },
  { kind: 'codex' as const, available: true, version: 'codex-cli 0.154.0', bin: 'codex' },
]

export const MOCK_HIDDEN_COMMANDS = ['clear', 'doctor', 'color', 'reload-plugins', 'model', 'effort']

export const MOCK_SLASH_COMMANDS = ['compact', 'commit-commands:commit', 'superpowers:brainstorming', 'clear', 'doctor']

// How long the mock waits before the resumed process "reports in" with a new
// init event, so the header's "Resuming with …" state is observable the way it
// is against the real CLI.
export const RESUME_DELAY_MS = 2000
// --- end T38 block ---------------------------------------------------------


export const sessions: Session[] = [
  {
    id: '00000000-0000-4000-8000-000000000002',
    owner_id: DEV_USER_ID,
    title: 'Investigate disk usage on prod-01',
    workspace_id: 'w1',
    profile_id: 'investigate',
    harness: 'claude',
    state: 'waiting',
    origin: 'ui',
    origin_ref: '',
    worktree: '',
    branch: '',
    base_ref: '',
    worktree_shared: false,
    diff_add: 0,
    diff_del: 0,
    created_at: iso(22),
    last_active_at: iso(2),
    num_turns: 3,
    cost_usd: 0.12,
    tokens_in: 4200,
    tokens_out: 380,
    now_line: 'Waiting on your decision for Bash',
    model: 'claude-fable-5-1',
    effort: 'medium',
    slash_commands: MOCK_SLASH_COMMANDS,
  },
  {
    id: MOCK_EVENT_SESSION_ID,
    owner_id: DEV_USER_ID,
    title: 'Refactor auth middleware',
    workspace_id: 'w1',
    profile_id: 'interactive',
    harness: 'claude',
    state: 'running',
    origin: 'ui',
    origin_ref: '',
    worktree: '',
    branch: '',
    base_ref: '',
    worktree_shared: false,
    diff_add: 7,
    diff_del: 3,
    created_at: iso(40),
    last_active_at: iso(0),
    num_turns: 5,
    cost_usd: 0.41,
    tokens_in: 15200,
    tokens_out: 2100,
    now_line: 'Editing src/auth.ts',
    model: 'claude-fable-5-1',
    effort: 'medium',
    slash_commands: MOCK_SLASH_COMMANDS,
  },
  {
    id: '00000000-0000-4000-8000-000000000004',
    owner_id: DEV_USER_ID,
    title: 'Summarize changelog',
    workspace_id: 'w2',
    profile_id: 'interactive',
    harness: 'claude',
    state: 'closed',
    origin: 'ui',
    origin_ref: '',
    worktree: '',
    branch: '',
    base_ref: '',
    worktree_shared: false,
    diff_add: 0,
    diff_del: 0,
    created_at: iso(180),
    last_active_at: iso(150),
    num_turns: 2,
    cost_usd: 0.05,
    tokens_in: 1800,
    tokens_out: 220,
    now_line: '',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: MOCK_SLASH_COMMANDS,
  },
  {
    id: '00000000-0000-4000-8000-000000000006',
    owner_id: DEV_USER_ID,
    title: 'Add health check endpoint',
    workspace_id: 'w1',
    profile_id: 'interactive',
    harness: 'claude',
    state: 'failed',
    origin: 'ui',
    origin_ref: '',
    worktree: '',
    branch: '',
    base_ref: '',
    worktree_shared: false,
    diff_add: 0,
    diff_del: 0,
    created_at: iso(400),
    last_active_at: iso(390),
    num_turns: 4,
    cost_usd: 0.18,
    tokens_in: 5200,
    tokens_out: 640,
    now_line: '',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: MOCK_SLASH_COMMANDS,
  },
  {
    // Fixed id (also hardcoded in e2e/session.spec.ts) so the Task 20 session
    // view spec can open it directly. Its transcript is fixture 02
    // (internal/harness/claude/testdata/02_tool_read.jsonl, copied to
    // ./fixtures/02_tool_read.jsonl and decoded below into harness.Event JSON):
    // a Read tool call followed by the text reply "hello".
    id: TOOL_FIXTURE_SESSION_ID,
    owner_id: DEV_USER_ID,
    title: 'Read note.txt',
    // Matches the seeded worktree diff in ./reviewState.ts; `session.stats`
    // moves these live against the real backend.
    diff_add: 42,
    diff_del: 18,
    workspace_id: 'w1',
    profile_id: 'interactive',
    harness: 'claude',
    state: 'open',
    origin: 'ui',
    origin_ref: '',
    // The one seeded session that runs in a worktree, matching the diff,
    // checkpoints and branch name in ./reviewState.ts.
    worktree: '/srv/styr/w1/.styr/worktrees/00000000-0000-4000-8000-000000000005',
    branch: 'styr/0000005-read-note-txt',
    base_ref: 'main',
    worktree_shared: false,
    created_at: iso(15),
    last_active_at: iso(10),
    num_turns: 2,
    cost_usd: 0.4624,
    tokens_in: 34,
    tokens_out: 111,
    now_line: '',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: MOCK_SLASH_COMMANDS,
  },
]
