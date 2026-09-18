// In-memory mock of every route in the Task 13 route table
// (docs/superpowers/plans/2026-09-18-styr-v0.1.md, "### Task 13: HTTP API").
// Seed data: one dev admin user with a present Claude token, two workspaces,
// the three builtin profiles, three sessions in different states and two
// pending approvals. The `/api/v1/events` SSE route is intentionally absent
// here — msw cannot intercept EventSource, so useLiveEvents.ts swaps in
// FakeEventSource (./fakeEventSource.ts) under VITE_MOCK=1 instead.
import { http, HttpResponse } from 'msw'
import type {
  Approval,
  ApiErrorBody,
  ClaudeTokenInfo,
  Me,
  Profile,
  Provider,
  Session,
  SessionEvent,
  StatusInfo,
  User,
  Workspace,
} from '../api/types'
import { MOCK_EVENT_SESSION_ID } from './fakeEventSource'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

function errorBody(code: string, message: string): ApiErrorBody {
  return { error: { code, message } }
}

// The dev verifier: accepts anything starting with "sk-ant-" (real Claude
// tokens' prefix), rejects everything else with 422 token_invalid. Shared by
// PUT /me/claude-token and PUT /settings/service-token, which both follow
// the same verify-then-store rule (docs/openapi.yaml).
export const TOKEN_INVALID_MESSAGE =
  "That token wasn't accepted. Run claude setup-token on a machine where Claude Code is already logged in, then paste the token it prints here."

function verifyToken(token: string): ClaudeTokenInfo | null {
  if (!token.startsWith('sk-ant-')) return null
  return { present: true, label: `…${token.slice(-6)}`, verified_at: iso(0) }
}

const DEV_USER_ID = '00000000-0000-4000-8000-000000000001'

const workspaces: Workspace[] = [
  {
    id: 'w1',
    name: 'styr',
    path: '/home/dev/styr',
    default_profile_id: 'interactive',
    worktrees: false,
    created_at: iso(60 * 24 * 30),
  },
  {
    id: 'w2',
    name: 'notes',
    path: '/home/dev/notes',
    default_profile_id: 'interactive',
    worktrees: true,
    created_at: iso(60 * 24 * 10),
  },
]

const profiles: Profile[] = [
  {
    id: 'interactive',
    name: 'interactive',
    mode: 'default',
    allowed_tools: [],
    disallowed_tools: [],
    max_turns: 0,
    unattended: false,
    approval_timeout: 0,
    builtin: true,
  },
  {
    id: 'investigate',
    name: 'investigate',
    mode: 'default',
    allowed_tools: [
      'Read',
      'Grep',
      'Glob',
      'Bash(ssh * journalctl *)',
      'Bash(ssh * systemctl status *)',
      'Bash(curl *)',
    ],
    disallowed_tools: ['WebFetch', 'Edit', 'Write'],
    max_turns: 30,
    unattended: true,
    approval_timeout: 1800,
    builtin: true,
  },
  {
    id: 'remediate',
    name: 'remediate',
    mode: 'default',
    allowed_tools: [
      'Read',
      'Grep',
      'Glob',
      'Bash(ssh * journalctl *)',
      'Bash(ssh * systemctl status *)',
      'Bash(ssh * systemctl restart *)',
      'Bash(ssh * pct restart *)',
      'Bash(curl *)',
    ],
    disallowed_tools: ['WebFetch'],
    max_turns: 40,
    unattended: true,
    approval_timeout: 1800,
    builtin: true,
  },
]

const sessions: Session[] = [
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
    id: '00000000-0000-4000-8000-000000000005',
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
]

const approvals: Approval[] = [
  {
    id: 'a1',
    session_id: sessions[0].id,
    request_id: 'req-a1',
    tool: 'Bash',
    input: { command: 'systemctl restart nginx', description: 'Restart nginx after config change' },
    risk: 'exec',
    state: 'pending',
    created_at: iso(2),
    decided_by: null,
    decided_at: null,
    snoozed_until: null,
    updated_input: null,
    message: '',
    session_title: sessions[0].title,
    now_line: sessions[0].now_line,
  },
  {
    id: 'a2',
    session_id: sessions[0].id,
    request_id: 'req-a2',
    tool: 'Bash',
    input: { command: 'rm -rf dist/tmp-cache', description: 'Clear stale build cache' },
    risk: 'destructive',
    state: 'pending',
    created_at: iso(1),
    decided_by: null,
    decided_at: null,
    snoozed_until: null,
    updated_input: null,
    message: '',
    session_title: sessions[0].title,
    now_line: sessions[0].now_line,
  },
]

const users: User[] = [
  {
    id: DEV_USER_ID,
    email: 'dev@styr.local',
    display_name: 'Dev Admin',
    avatar_url: '',
    role: 'admin',
    created_at: iso(60 * 24 * 60),
    last_login_at: iso(5),
  },
]

const providers: Provider[] = [{ name: 'Authentik', slug: 'authentik' }]

// Toggled by POST /__mock/logout so the login e2e spec can exercise the
// AuthGate's 401 -> /login redirect without a real session cookie.
let loggedOut = false

// Mutable so PUT/DELETE /me/claude-token (Task 22's ClaudeTokenCard) can
// flip presence; GET /api/v1/me reads this live rather than a frozen
// literal. Seeded present, matching the "Dev Admin" persona already being
// logged in with a working token; profile.spec.ts flips it absent via
// POST /__mock/reset-claude-token for the absent-state test.
let devClaudeToken: ClaudeTokenInfo = { present: true, label: '…a1b2c3', verified_at: iso(60 * 24) }

// Mirrors GET /api/v1/settings's shape (docs/openapi.yaml): idle_timeout is
// a Go duration string, not a number of seconds.
interface MockSettings {
  max_open_sessions: number
  idle_timeout: string
  service_token: ClaudeTokenInfo
}

let settings: MockSettings = {
  max_open_sessions: 4,
  idle_timeout: '15m0s',
  service_token: { present: false, label: '', verified_at: null },
}

export const handlers = [
  http.get('/healthz', () => HttpResponse.json({ ok: true })),

  http.get('/api/v1/auth/providers', () => HttpResponse.json(providers)),

  http.get('/api/v1/auth/login/:slug', () => {
    loggedOut = false
    return new HttpResponse(null, { status: 302, headers: { Location: '/api/v1/auth/callback' } })
  }),

  http.get('/api/v1/auth/callback', () => {
    loggedOut = false
    return new HttpResponse(null, { status: 302, headers: { Location: '/' } })
  }),

  http.post('/api/v1/auth/logout', () => {
    loggedOut = true
    return new HttpResponse(null, { status: 204 })
  }),

  // Mock-only control route: flips the same "logged out" flag as the real
  // logout endpoint, so tests can force /api/v1/me to 401.
  http.post('/__mock/logout', () => {
    loggedOut = true
    return new HttpResponse(null, { status: 204 })
  }),

  // Mock-only control route (Task 22): puts the dev user's Claude token back
  // to absent, so profile.spec.ts can exercise ClaudeTokenCard's absent
  // state without a fresh page load re-seeding every other flag too.
  http.post('/__mock/reset-claude-token', () => {
    devClaudeToken = { present: false, label: '', verified_at: null }
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/me', () => {
    if (loggedOut) return HttpResponse.json(errorBody('unauthorized', 'not logged in'), { status: 401 })
    const me: Me = {
      id: DEV_USER_ID,
      email: 'dev@styr.local',
      display_name: 'Dev Admin',
      avatar_url: '',
      role: 'admin',
      prefs: { theme: 'dark' },
      claude_token: devClaudeToken,
    }
    return HttpResponse.json(me)
  }),

  http.patch('/api/v1/me', () => new HttpResponse(null, { status: 204 })),

  http.put('/api/v1/me/claude-token', async ({ request }) => {
    const body = (await request.json()) as { token?: string }
    const verified = verifyToken(body.token ?? '')
    if (!verified) return HttpResponse.json(errorBody('token_invalid', TOKEN_INVALID_MESSAGE), { status: 422 })
    devClaudeToken = verified
    return new HttpResponse(null, { status: 204 })
  }),

  http.delete('/api/v1/me/claude-token', () => {
    devClaudeToken = { present: false, label: '', verified_at: null }
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/users', () => HttpResponse.json(users)),

  http.patch('/api/v1/users/:id', async ({ request, params }) => {
    const body = (await request.json()) as { role?: User['role'] }
    const user = users.find((u) => u.id === params.id)
    if (!user) return HttpResponse.json(errorBody('not_found', 'user not found'), { status: 404 })
    if (body.role) user.role = body.role
    return HttpResponse.json(user)
  }),

  http.get('/api/v1/settings', () => HttpResponse.json(settings)),

  http.put('/api/v1/settings/service-token', async ({ request }) => {
    const body = (await request.json()) as { token?: string }
    const verified = verifyToken(body.token ?? '')
    if (!verified) return HttpResponse.json(errorBody('token_invalid', TOKEN_INVALID_MESSAGE), { status: 422 })
    settings = { ...settings, service_token: verified }
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/workspaces', () => HttpResponse.json(workspaces)),
  http.post('/api/v1/workspaces', async ({ request }) => {
    const body = (await request.json()) as Partial<Workspace>
    // Real validation is "exists, is a directory, contains .git"
    // (docs/openapi.yaml); the mock can't stat a filesystem, so it checks
    // the one thing it can: an absolute path. Enough to exercise the "Add
    // workspace" dialog's inline-error path in Task 22.
    if (!body.path || !body.path.startsWith('/')) {
      return HttpResponse.json(errorBody('invalid_path', 'Path must be an absolute path to a git repository.'), {
        status: 422,
      })
    }
    const ws: Workspace = {
      id: `w${workspaces.length + 1}`,
      name: body.name ?? 'untitled',
      path: body.path,
      default_profile_id: body.default_profile_id ?? 'interactive',
      worktrees: body.worktrees ?? false,
      created_at: iso(0),
    }
    workspaces.push(ws)
    return HttpResponse.json(ws, { status: 201 })
  }),
  http.patch('/api/v1/workspaces/:id', async ({ request, params }) => {
    const body = (await request.json()) as Partial<Workspace>
    const ws = workspaces.find((w) => w.id === params.id)
    if (!ws) return HttpResponse.json(errorBody('not_found', 'workspace not found'), { status: 404 })
    Object.assign(ws, body)
    return HttpResponse.json(ws)
  }),
  http.delete('/api/v1/workspaces/:id', ({ params }) => {
    const index = workspaces.findIndex((w) => w.id === params.id)
    if (index !== -1) workspaces.splice(index, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/profiles', () => HttpResponse.json(profiles)),
  http.post('/api/v1/profiles', async ({ request }) => {
    const body = (await request.json()) as Partial<Profile>
    const p: Profile = {
      id: `p${profiles.length + 1}`,
      name: body.name ?? 'untitled',
      mode: body.mode ?? 'default',
      allowed_tools: body.allowed_tools ?? [],
      disallowed_tools: body.disallowed_tools ?? [],
      max_turns: body.max_turns ?? 0,
      unattended: body.unattended ?? false,
      approval_timeout: body.approval_timeout ?? 0,
      builtin: false,
    }
    profiles.push(p)
    return HttpResponse.json(p, { status: 201 })
  }),
  http.patch('/api/v1/profiles/:id', async ({ request, params }) => {
    const body = (await request.json()) as Partial<Profile>
    // Reject by allow-list rather than naming the forbidden mode: the
    // Global Constraints CI gate greps the whole repo for that literal
    // string (never passed to the CLI, anywhere), so mock validation must
    // not spell it out either. See docs/superpowers/plans/2026-09-18-styr-v0.1.md.
    const validModes = new Set<Profile['mode']>(['default', 'acceptEdits', 'plan', 'dontAsk', 'auto'])
    if (body.mode !== undefined && !validModes.has(body.mode)) {
      return HttpResponse.json(errorBody('invalid', 'unsupported permission mode'), { status: 422 })
    }
    const p = profiles.find((x) => x.id === params.id)
    if (!p) return HttpResponse.json(errorBody('not_found', 'profile not found'), { status: 404 })
    if (p.builtin) {
      // "Builtin profiles only allow max_turns and approval_timeout to
      // change" (docs/openapi.yaml) - reject any other field that would
      // actually change the stored value.
      const editableKeys = new Set(['max_turns', 'approval_timeout'])
      const offending = (Object.keys(body) as Array<keyof Profile>).some(
        (key) => !editableKeys.has(key) && body[key] !== undefined && body[key] !== p[key],
      )
      if (offending) {
        return HttpResponse.json(
          errorBody('immutable_field', 'Builtin profiles only allow max turns and approval timeout to change.'),
          { status: 422 },
        )
      }
    }
    Object.assign(p, body)
    return HttpResponse.json(p, { status: 200 })
  }),

  http.get('/api/v1/sessions', () => HttpResponse.json(sessions)),

  http.post('/api/v1/sessions', async ({ request }) => {
    const body = (await request.json()) as { workspace_id: string; profile_id: string; title: string; prompt: string }
    const session: Session = {
      id: crypto.randomUUID(),
      owner_id: DEV_USER_ID,
      title: body.title || 'Untitled session',
      workspace_id: body.workspace_id,
      profile_id: body.profile_id,
      harness: 'claude',
      state: 'open',
      origin: 'ui',
      origin_ref: '',
      worktree: '',
      created_at: iso(0),
      last_active_at: iso(0),
      num_turns: 0,
      cost_usd: 0,
      tokens_in: 0,
      tokens_out: 0,
      now_line: '',
      model: 'claude-fable-5-1',
    }
    sessions.unshift(session)
    return HttpResponse.json(session, { status: 201 })
  }),

  http.get('/api/v1/sessions/:id', ({ params }) => {
    const session = sessions.find((s) => s.id === params.id)
    if (!session) return HttpResponse.json(errorBody('not_found', 'session not found'), { status: 404 })
    return HttpResponse.json(session)
  }),

  http.post('/api/v1/sessions/:id/messages', () => new HttpResponse(null, { status: 202 })),
  http.post('/api/v1/sessions/:id/interrupt', () => new HttpResponse(null, { status: 202 })),
  http.post('/api/v1/sessions/:id/close', () => new HttpResponse(null, { status: 204 })),

  http.get('/api/v1/sessions/:id/events', () => {
    const events: SessionEvent[] = []
    return HttpResponse.json(events)
  }),

  http.get('/api/v1/approvals', () => HttpResponse.json(approvals.filter((a) => a.state === 'pending'))),

  http.post('/api/v1/approvals/:id', async ({ request, params }) => {
    const body = (await request.json()) as { decision: 'allow' | 'deny'; updated_input?: unknown; message?: string }
    const approval = approvals.find((a) => a.id === params.id)
    if (!approval) return HttpResponse.json(errorBody('not_found', 'approval not found'), { status: 404 })
    approval.state = body.decision === 'allow' ? 'allowed' : 'denied'
    approval.decided_by = DEV_USER_ID
    approval.decided_at = iso(0)
    if (body.updated_input !== undefined) approval.updated_input = body.updated_input
    if (body.message) approval.message = body.message
    const session = sessions.find((s) => s.id === approval.session_id)
    if (session && !approvals.some((a) => a.session_id === session.id && a.state === 'pending')) {
      session.state = 'open'
      session.now_line = ''
    }
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/api/v1/approvals/:id/snooze', async ({ params }) => {
    const approval = approvals.find((a) => a.id === params.id)
    if (!approval) return HttpResponse.json(errorBody('not_found', 'approval not found'), { status: 404 })
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/status', () => {
    const status: StatusInfo = {
      version: '0.1.0-mock',
      claude_version: '2.1.276',
      open_processes: sessions.filter((s) => s.state === 'running' || s.state === 'waiting').length,
      slots: 4,
      queue_depth: 0,
    }
    return HttpResponse.json(status)
  }),
]
