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
  Role,
  User,
  Workspace,
} from '../api/types'
import { MOCK_EVENT_SESSION_ID, emitFakeEvent } from './fakeEventSource'
import { decodeFixtureEvents } from './decodeFixture'
import fixture02Raw from './fixtures/02_tool_read.jsonl?raw'
import type { HarnessEventPayload } from '../lib/blocks'
// T36 (personal API tokens): kept as its own import line rather than folded
// into the block above so this card's additive changes don't collide with
// another card's edit to that same import statement.
import type { ApiToken, ApiTokenCreated } from '../api/types'

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

// Fixed id for the Task 20 seeded session (fixture 02: a Read tool call then
// "hello"). Hardcoded rather than exported: e2e specs run outside Vite, so
// they can't import a module that pulls in a `?raw` fixture import.
export const TOOL_FIXTURE_SESSION_ID = '00000000-0000-4000-8000-000000000005'

// Per-session transcript event log. Seeded once at module load for the
// fixture session; POST /messages appends to whichever session it targets.
const eventsBySession = new Map<string, SessionEvent[]>()

function seedEvents(sessionId: string, payloads: HarnessEventPayload[]) {
  const events: SessionEvent[] = payloads.map((payload, i) => ({
    id: i + 1,
    session_id: sessionId,
    seq: i + 1,
    at: payload.At ?? iso(0),
    type: payload.Type,
    payload,
  }))
  eventsBySession.set(sessionId, events)
}

seedEvents(TOOL_FIXTURE_SESSION_ID, decodeFixtureEvents(fixture02Raw, new Date(Date.now() - 10 * 60_000)))

function appendEvent(sessionId: string, payload: HarnessEventPayload): SessionEvent {
  const list = eventsBySession.get(sessionId) ?? []
  const event: SessionEvent = {
    id: list.length + 1,
    session_id: sessionId,
    seq: list.length + 1,
    at: payload.At ?? iso(0),
    type: payload.Type,
    payload,
  }
  eventsBySession.set(sessionId, [...list, event])
  return event
}

// Per-user workspaces (T29): Styr owns the path for "git" and "empty" - only
// a "path" source (an admin registering a checkout that already exists on
// this machine) carries one worth showing, and only to an admin. Both seeds
// are owned by the one dev account and already "ready", so the rest of the
// e2e suite (session creation, NewSessionDialog defaults, ...) has something
// to start a session in without waiting on a simulated clone.
const workspaces: Workspace[] = [
  {
    id: 'w1',
    owner_id: DEV_USER_ID,
    name: 'styr',
    path: '/data/workspaces/w1',
    source: 'git',
    repo_url: 'https://github.com/styr-dev/styr.git',
    branch: 'main',
    managed: true,
    state: 'ready',
    error: '',
    default_profile_id: 'interactive',
    worktrees: false,
    created_at: iso(60 * 24),
    updated_at: iso(60 * 24),
  },
  {
    id: 'w2',
    owner_id: DEV_USER_ID,
    name: 'notes',
    path: '/data/workspaces/w2',
    source: 'empty',
    repo_url: null,
    branch: null,
    managed: true,
    state: 'ready',
    error: '',
    default_profile_id: 'interactive',
    worktrees: true,
    created_at: iso(60 * 24),
    updated_at: iso(60 * 24),
  },
]

let nextWorkspaceSeq = workspaces.length + 1

// Real clone attempts take a moment; the mock simulates the same "cloning"
// -> "ready"/"failed" transition on a short timer, publishing the same
// workspace.state SSE frame docs/openapi.yaml specifies so the live-patch
// path (useLiveEvents.ts) is exercised the same way in mock and real modes.
const CLONE_DELAY_MS = 1500

function publishWorkspaceState(workspace: Workspace) {
  emitFakeEvent('workspace.state', {
    kind: 'workspace.state',
    payload: { id: workspace.id, state: workspace.state, error: workspace.error },
  })
}

function scheduleCloneOutcome(workspace: Workspace) {
  setTimeout(() => {
    if (workspace.repo_url?.includes('fail')) {
      workspace.state = 'failed'
      workspace.error = 'fatal: repository not found'
    } else {
      workspace.state = 'ready'
      workspace.error = null
    }
    workspace.updated_at = iso(0)
    publishWorkspaceState(workspace)
  }, CLONE_DELAY_MS)
}

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
  {
    // One editable profile alongside the three builtins. Builtin rows lock
    // their name and mode, so without this there is no unlocked mode select
    // for a spec to open and read the five allowed modes out of.
    id: 'custom',
    name: 'custom',
    mode: 'default',
    allowed_tools: [],
    disallowed_tools: [],
    max_turns: 0,
    unattended: false,
    approval_timeout: 0,
    builtin: false,
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
    prefs: {},
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

// Flipped by POST /__mock/set-role so a spec can see a members-only view
// (the first-run "ask an admin" copy) without a second seeded user.
let devRole: Role = 'admin'

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

// ---- T36: personal API tokens (additive; see the matching block appended
// at the end of `handlers` below) ------------------------------------------
// Starts empty: e2e/api-tokens.spec.ts creates its own tokens rather than
// asserting against pre-seeded ones, so a fresh page load always starts
// from "no tokens yet".
let nextApiTokenSeq = 1
const apiTokens: ApiToken[] = []

function createApiToken(name: string, expiresInDays?: number): ApiTokenCreated {
  const id = `apitok-${nextApiTokenSeq++}`
  // Shaped like a real secret (auth.GenerateAPIToken's APITokenPrefix) so
  // anything in the UI that only checks the prefix behaves the same in
  // mock and real mode; not a real random secret since this never leaves
  // the browser tab running the mock.
  const secret = `styr_pat_mock${id}${Math.random().toString(36).slice(2, 10)}`
  const token: ApiToken = {
    id,
    name,
    prefix: secret.slice(0, 12),
    created_at: iso(0),
    last_used_at: null,
    expires_at: expiresInDays ? new Date(Date.now() + expiresInDays * 24 * 60 * 60 * 1000).toISOString() : null,
  }
  apiTokens.unshift(token)
  return { id: token.id, name: token.name, prefix: token.prefix, token: secret }
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

  // Gap 2: "Sign out everywhere" (Profile.tsx). The mock has no per-device
  // session list to revoke individually, so this just ends the one session
  // a browser can have, same as /auth/logout.
  http.post('/api/v1/auth/logout-all', () => {
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

  // Mock-only control routes for the first-run path: a server with nothing
  // registered yet, seen once as an admin (who can fix it from here) and once
  // as a member (who can only ask someone).
  http.post('/__mock/clear-workspaces', () => {
    workspaces.splice(0, workspaces.length)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/__mock/set-role', async ({ request }) => {
    const body = (await request.json()) as { role?: Role }
    if (body.role === 'admin' || body.role === 'member') devRole = body.role
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/me', () => {
    if (loggedOut) return HttpResponse.json(errorBody('unauthorized', 'not logged in'), { status: 401 })
    const me: Me = {
      id: DEV_USER_ID,
      email: 'dev@styr.local',
      display_name: 'Dev Admin',
      avatar_url: '',
      role: devRole,
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

  // Visible workspaces: an admin sees every one, everyone else sees their
  // own plus the shared (owner_id null) ones - a "path" source an admin
  // registered for a repo that only exists on this box, per the product
  // decision that Styr otherwise owns every workspace's path.
  http.get('/api/v1/workspaces', () => {
    const visible = devRole === 'admin' ? workspaces : workspaces.filter((w) => w.owner_id === null || w.owner_id === DEV_USER_ID)
    return HttpResponse.json(visible)
  }),

  http.post('/api/v1/workspaces', async ({ request }) => {
    const body = (await request.json()) as {
      name?: string
      source?: Workspace['source']
      repo_url?: string
      branch?: string
      path?: string
      default_profile_id?: string
      worktrees?: boolean
    }
    const source = body.source
    if (source === 'path' && devRole !== 'admin') {
      return HttpResponse.json(errorBody('forbidden', 'Only an admin can register a server path.'), { status: 403 })
    }
    if (!body.name || !body.name.trim()) {
      return HttpResponse.json(errorBody('invalid', 'Name is required.'), { status: 422 })
    }
    if (workspaces.some((w) => w.name === body.name)) {
      return HttpResponse.json(errorBody('name_taken', `A workspace named "${body.name}" already exists.`), { status: 422 })
    }
    if (source === 'git' && (!body.repo_url || !body.repo_url.trim())) {
      return HttpResponse.json(errorBody('invalid_url', 'Enter a repository URL to clone.'), { status: 422 })
    }
    if (source === 'path' && (!body.path || !body.path.startsWith('/'))) {
      return HttpResponse.json(errorBody('invalid_path', 'Path must be an absolute path on the machine running Styr.'), {
        status: 422,
      })
    }

    const id = `w${nextWorkspaceSeq}`
    nextWorkspaceSeq += 1
    const now = iso(0)
    const workspace: Workspace = {
      id,
      // A registered server path is a shared resource (there was no such
      // thing as a personal one before this card); a git clone or an empty
      // folder is Styr's own managed checkout, scoped to whoever asked for it.
      owner_id: source === 'path' ? null : DEV_USER_ID,
      name: body.name,
      path: source === 'path' ? (body.path as string) : `/data/workspaces/${id}`,
      source: source ?? 'empty',
      repo_url: source === 'git' ? (body.repo_url as string) : null,
      branch: source === 'git' ? body.branch || null : null,
      managed: source !== 'path',
      state: source === 'git' ? 'cloning' : 'ready',
      error: '',
      default_profile_id: body.default_profile_id || 'interactive',
      worktrees: body.worktrees ?? false,
      created_at: now,
      updated_at: now,
    }
    workspaces.push(workspace)
    if (workspace.state === 'cloning') scheduleCloneOutcome(workspace)
    return HttpResponse.json(workspace, { status: 201 })
  }),

  http.get('/api/v1/workspaces/:id', ({ params }) => {
    const workspace = workspaces.find((w) => w.id === params.id)
    if (!workspace) return HttpResponse.json(errorBody('not_found', 'workspace not found'), { status: 404 })
    return HttpResponse.json(workspace)
  }),

  http.patch('/api/v1/workspaces/:id', async ({ request, params }) => {
    const body = (await request.json()) as { default_profile_id?: string; worktrees?: boolean }
    const workspace = workspaces.find((w) => w.id === params.id)
    if (!workspace) return HttpResponse.json(errorBody('not_found', 'workspace not found'), { status: 404 })
    if (body.default_profile_id !== undefined) workspace.default_profile_id = body.default_profile_id
    if (body.worktrees !== undefined) workspace.worktrees = body.worktrees
    workspace.updated_at = iso(0)
    return HttpResponse.json(workspace)
  }),

  http.delete('/api/v1/workspaces/:id', ({ params }) => {
    const workspace = workspaces.find((w) => w.id === params.id)
    if (!workspace) return new HttpResponse(null, { status: 204 })
    const OPEN_STATES = new Set(['open', 'running', 'waiting'])
    const hasOpenSessions = sessions.some((s) => s.workspace_id === workspace.id && OPEN_STATES.has(s.state))
    if (hasOpenSessions) {
      return HttpResponse.json(errorBody('sessions_open', 'This workspace has open sessions.'), { status: 409 })
    }
    const index = workspaces.findIndex((w) => w.id === params.id)
    workspaces.splice(index, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/api/v1/workspaces/:id/retry', ({ params }) => {
    const workspace = workspaces.find((w) => w.id === params.id)
    if (!workspace) return HttpResponse.json(errorBody('not_found', 'workspace not found'), { status: 404 })
    workspace.state = 'cloning'
    workspace.error = null
    workspace.updated_at = iso(0)
    publishWorkspaceState(workspace)
    scheduleCloneOutcome(workspace)
    return new HttpResponse(null, { status: 202 })
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

  // Real Send() only decodes harness.Event output from the CLI: the operator's
  // own turn is never persisted as a transcript event (PROTOCOL.md — the CLI's
  // echo of it is deliberately dropped by the codec). The mock still needs the
  // transcript to show what was sent, so it synthesises a `user`-typed event —
  // a mock-only convention lib/blocks.ts's foldEvents understands (see its
  // module comment) — and the session view refetches events after sending.
  http.post('/api/v1/sessions/:id/messages', async ({ request, params }) => {
    const body = (await request.json()) as { text: string }
    const id = params.id as string
    const session = sessions.find((s) => s.id === id)
    if (!session) return HttpResponse.json(errorBody('not_found', 'session not found'), { status: 404 })
    appendEvent(id, { Type: 'user', At: iso(0), Text: body.text })
    session.state = 'open'
    session.last_active_at = iso(0)
    return new HttpResponse(null, { status: 202 })
  }),
  http.post('/api/v1/sessions/:id/interrupt', () => new HttpResponse(null, { status: 202 })),
  http.post('/api/v1/sessions/:id/close', ({ params }) => {
    const session = sessions.find((s) => s.id === params.id)
    if (session) session.state = 'closed'
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/sessions/:id/events', ({ request, params }) => {
    const url = new URL(request.url)
    const after = Number(url.searchParams.get('after') ?? '0')
    const limit = Number(url.searchParams.get('limit') ?? '500')
    const events = (eventsBySession.get(params.id as string) ?? []).filter((e) => e.seq > after).slice(0, limit)
    return HttpResponse.json(events)
  }),

  http.get('/api/v1/approvals', () => HttpResponse.json(approvals.filter((a) => a.state === 'pending'))),

  http.post('/api/v1/approvals/:id', async ({ request, params }) => {
    const body = (await request.json()) as { decision: 'allow' | 'deny'; updated_input?: unknown; message?: string }
    const approval = approvals.find((a) => a.id === params.id)
    if (!approval) return HttpResponse.json(errorBody('not_found', 'approval not found'), { status: 404 })
    approval.state = body.decision === 'allow' ? 'allowed' : 'denied'
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

  // ---- T36: personal API tokens (additive block; see the apiTokens seed
  // and createApiToken helper above `handlers`) ----------------------------
  http.get('/api/v1/me/api-tokens', () => HttpResponse.json(apiTokens)),

  http.post('/api/v1/me/api-tokens', async ({ request }) => {
    const body = (await request.json()) as { name?: string; expires_in_days?: number }
    const name = body.name?.trim()
    if (!name) return HttpResponse.json(errorBody('invalid', 'name is required'), { status: 422 })
    return HttpResponse.json(createApiToken(name, body.expires_in_days), { status: 201 })
  }),

  http.delete('/api/v1/me/api-tokens/:id', ({ params }) => {
    const idx = apiTokens.findIndex((t) => t.id === params.id)
    if (idx === -1) return HttpResponse.json(errorBody('not_found', 'token not found'), { status: 404 })
    apiTokens.splice(idx, 1)
    return new HttpResponse(null, { status: 204 })
  }),
]
