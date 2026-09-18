// Shared mutable session seed state, split out of handlers.ts so
// triggersHandlers.ts (T33) can seed webhook-origin sessions for its three
// seeded runs and read them back for GET /runs/{id}'s embedded session
// summary, without a circular import between the two handler modules.
// handlers.ts still owns every /api/v1/sessions* route; this module only
// owns the backing array and the ids other seed data needs to reference.
import type { Session } from '../api/types'
import { MOCK_EVENT_SESSION_ID } from './fakeEventSource'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

export const DEV_USER_ID = '00000000-0000-4000-8000-000000000001'

// Fixed id for the Task 20 seeded session (fixture 02: a Read tool call then
// "hello"). Hardcoded rather than exported for e2e: e2e specs run outside
// Vite, so they can't import a module that pulls in a `?raw` fixture import.
export const TOOL_FIXTURE_SESSION_ID = '00000000-0000-4000-8000-000000000005'

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
    created_at: iso(22),
    last_active_at: iso(2),
    num_turns: 3,
    cost_usd: 0.12,
    tokens_in: 4200,
    tokens_out: 380,
    now_line: 'Waiting on your decision for Bash',
    model: 'claude-fable-5-1',
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
    created_at: iso(40),
    last_active_at: iso(0),
    num_turns: 5,
    cost_usd: 0.41,
    tokens_in: 15200,
    tokens_out: 2100,
    now_line: 'Editing src/auth.ts',
    model: 'claude-fable-5-1',
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
    created_at: iso(180),
    last_active_at: iso(150),
    num_turns: 2,
    cost_usd: 0.05,
    tokens_in: 1800,
    tokens_out: 220,
    now_line: '',
    model: 'claude-fable-5-1',
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
    created_at: iso(400),
    last_active_at: iso(390),
    num_turns: 4,
    cost_usd: 0.18,
    tokens_in: 5200,
    tokens_out: 640,
    now_line: '',
    model: 'claude-fable-5-1',
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
    workspace_id: 'w1',
    profile_id: 'interactive',
    harness: 'claude',
    state: 'open',
    origin: 'ui',
    origin_ref: '',
    worktree: '',
    created_at: iso(15),
    last_active_at: iso(10),
    num_turns: 2,
    cost_usd: 0.4624,
    tokens_in: 34,
    tokens_out: 111,
    now_line: '',
    model: 'claude-fable-5-1',
  },
]
