// Shared e2e seeding helper: works against both the mock backend (a no-op
// that hands back the mock's own fixed ids, see web/src/mocks/handlers.ts)
// and the real backend (docs/superpowers/plans/2026-09-18-styr-v0.1.md,
// integration wave). Real mode drives the actual HTTP API with
// `page.request` so requests share the browser context's dev-login cookie,
// exactly like the app's own fetches, and always sends
// `X-Requested-With: styr` (docs/superpowers/plans/..., "Frontend
// conventions").
//
// Fixture selection on the real backend goes through testdata/fake-claude/
// fake-claude.sh's "[fixture:NN]" marker (see its own comment): a prompt
// carrying the marker replays internal/harness/claude/testdata/NN_*.jsonl
// for that turn instead of the shell fake's default fixture.
import path from 'node:path'
import type { Page, TestInfo } from '@playwright/test'

const HEADERS = { 'X-Requested-With': 'styr' }

// The dev token verifier (cmd/styr/wire.go's devVerifier) accepts anything
// shaped like a real Claude token or OAuth credential ("sk-ant-" prefix)
// without spawning a process.
const REAL_TOKEN = 'sk-ant-oat01-e2e-token'

// The "interactive" builtin profile (internal/db/migrations/00001_init.sql):
// mode "default", no allowed/disallowed tool overrides, unattended false.
// Every seeded real session uses it: fixture 02's Read call and fixture
// 03's Bash call both need to ask permission (mode "default", not
// pre-allowed), matching what the mock's own "interactive" profile session
// (Task 20's tool fixture) and "investigate" profile session (Task 18's
// approvals) exercise the UI for.
const REAL_PROFILE_ID = 'interactive'

// Mock-seeded ids (web/src/mocks/handlers.ts) this helper falls back to
// off the real backend. Duplicated as literals rather than imported: e2e
// specs run outside Vite, so (like TOOL_FIXTURE_SESSION_ID in handlers.ts's
// own comment) they can't pull in a module that imports a `?raw` fixture.
const MOCK_WORKSPACE_ID = 'w1'
const MOCK_WAITING_SESSION_ID = '00000000-0000-4000-8000-000000000002'
const MOCK_TOOL_SESSION_ID = '00000000-0000-4000-8000-000000000005'
const MOCK_CLOSED_SESSION_TITLE = 'Summarize changelog'

export function isReal(testInfo: TestInfo): boolean {
  return testInfo.project.name.startsWith('real')
}

interface MinimalSession {
  id: string
  state: string
  title: string
}

interface MinimalWorkspace {
  id: string
  path: string
}

async function ok(res: { ok(): boolean; status(): number; text(): Promise<string> }, what: string): Promise<void> {
  if (!res.ok()) {
    throw new Error(`seed: ${what} failed: ${res.status()} ${await res.text()}`)
  }
}

// ensureRealToken (idempotent) makes sure the dev user has a working Claude
// token, required before the real backend accepts any session creation
// (internal/sessions/service.go's Create fails fast without one).
export async function ensureRealToken(page: Page): Promise<void> {
  const res = await page.request.put('/api/v1/me/claude-token', { headers: HEADERS, data: { token: REAL_TOKEN } })
  await ok(res, 'PUT /me/claude-token')
}

// Cached for the lifetime of the worker process (the real projects run with
// workers: 1 - see playwright.config.ts - specifically so this in-memory
// cache, and every other cross-test assumption about a single shared real
// backend, stays valid without a database round trip per test).
let realWorkspaceId: string | null = null

// ensureRealWorkspace (idempotent) returns a workspace pointing at this
// repository's root (validated server-side by handleWorkspacesCreate: must
// exist, be a directory, contain .git), creating it once per run.
export async function ensureRealWorkspace(page: Page): Promise<string> {
  if (realWorkspaceId) return realWorkspaceId
  const repoRoot = path.resolve(process.cwd(), '..')
  const res = await page.request.post('/api/v1/workspaces', {
    headers: HEADERS,
    data: { name: 'styr-e2e', path: repoRoot, default_profile_id: REAL_PROFILE_ID },
  })
  if (res.status() === 201) {
    const ws = (await res.json()) as MinimalWorkspace
    realWorkspaceId = ws.id
    return realWorkspaceId
  }
  // A previous run's database from before this run's fresh STYR_DATA_DIR
  // can't leak in (playwright.config.ts gives every real run its own data
  // dir), so this only guards a same-run race under a misconfigured
  // workers setting: fall back to whatever "styr-e2e" workspace exists.
  const list = await page.request.get('/api/v1/workspaces', { headers: HEADERS })
  await ok(list, 'GET /workspaces')
  const workspaces = (await list.json()) as Array<{ id: string; name: string }>
  const existing = workspaces.find((w) => w.name === 'styr-e2e')
  if (!existing) throw new Error(`seed: could not create or find the e2e workspace: ${res.status()} ${await res.text()}`)
  realWorkspaceId = existing.id
  return realWorkspaceId
}

async function createRealSession(page: Page, title: string, prompt: string): Promise<MinimalSession> {
  await ensureRealToken(page)
  const workspaceId = await ensureRealWorkspace(page)
  const res = await page.request.post('/api/v1/sessions', {
    headers: HEADERS,
    data: { workspace_id: workspaceId, profile_id: REAL_PROFILE_ID, title, prompt },
  })
  await ok(res, 'POST /sessions')
  return (await res.json()) as MinimalSession
}

async function waitForSessionState(page: Page, id: string, states: string[], timeoutMs = 20_000): Promise<MinimalSession> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const res = await page.request.get(`/api/v1/sessions/${id}`, { headers: HEADERS })
    if (res.ok()) {
      const sess = (await res.json()) as MinimalSession
      if (states.includes(sess.state)) return sess
      if (sess.state === 'failed') throw new Error(`seed: session ${id} failed while waiting for ${states.join('|')}`)
    }
    if (Date.now() > deadline) throw new Error(`seed: session ${id} did not reach ${states.join('|')} within ${timeoutMs}ms`)
    await new Promise((r) => setTimeout(r, 150))
  }
}

export interface ToolSession {
  workspaceId: string
  sessionId: string
}

// seedToolSession: a session whose transcript is fixture 02 (a Read tool
// call on note.txt, followed by the text reply "hello"), in `open` state.
// Mirrors web/src/mocks/handlers.ts's TOOL_FIXTURE_SESSION_ID session; used
// by e2e/session.spec.ts.
export async function seedToolSession(page: Page, testInfo: TestInfo): Promise<ToolSession> {
  if (!isReal(testInfo)) return { workspaceId: MOCK_WORKSPACE_ID, sessionId: MOCK_TOOL_SESSION_ID }
  const workspaceId = await ensureRealWorkspace(page)
  const sess = await createRealSession(page, 'Read note.txt', 'Read note.txt and reply with its content only. [fixture:02]')
  await waitForSessionState(page, sess.id, ['open'])
  return { workspaceId, sessionId: sess.id }
}

export interface WaitingSession {
  workspaceId: string
  sessionId: string
}

// seedWaitingSession: a fresh session carrying fixture 03's pending Bash
// permission request, in `waiting` state with exactly one pending approval.
// Used by e2e/inbox.spec.ts.
export async function seedWaitingSession(page: Page, testInfo: TestInfo): Promise<WaitingSession> {
  if (!isReal(testInfo)) return { workspaceId: MOCK_WORKSPACE_ID, sessionId: MOCK_WAITING_SESSION_ID }
  const workspaceId = await ensureRealWorkspace(page)
  const sess = await createRealSession(page, 'Run a shell command', 'Run a shell command using the Bash tool. [fixture:03]')
  await waitForSessionState(page, sess.id, ['waiting'])
  return { workspaceId, sessionId: sess.id }
}

// seedClosedSession: a session that reaches `closed` shortly after
// creation (fixture 01, then an explicit close), for the Inbox FYI section
// and the Sessions list's "Closed" group. Its title is unique per call so
// assertions can key off it instead of an exact list count, which would
// otherwise be fragile against every other spec seeding its own sessions
// into the same shared real backend.
export async function seedClosedSession(page: Page, testInfo: TestInfo): Promise<{ title: string }> {
  if (!isReal(testInfo)) return { title: MOCK_CLOSED_SESSION_TITLE }
  const title = `Say pong ${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
  const sess = await createRealSession(page, title, 'Reply with exactly the word pong and nothing else. [fixture:01]')
  await waitForSessionState(page, sess.id, ['open'])
  const res = await page.request.post(`/api/v1/sessions/${sess.id}/close`, { headers: HEADERS })
  await ok(res, 'POST /sessions/{id}/close')
  return { title }
}

// approvalIdForSession looks up the single pending approval for a session
// seeded by seedWaitingSession, so real-mode specs can assert against it
// specifically instead of the whole account's approvals list.
export async function approvalIdForSession(page: Page, sessionId: string): Promise<string> {
  const res = await page.request.get('/api/v1/approvals', { headers: HEADERS })
  await ok(res, 'GET /approvals')
  const approvals = (await res.json()) as Array<{ id: string; session_id: string }>
  const found = approvals.find((a) => a.session_id === sessionId)
  if (!found) throw new Error(`seed: no pending approval found for session ${sessionId}`)
  return found.id
}

// denyPendingApproval resolves a session's own pending approval (deny,
// never allow, so it never runs the recorded Bash command outside the
// fixture replay a test is actually exercising). Real-mode tests that seed
// a `waiting` session but don't otherwise decide its approval through the
// UI must call this before finishing: the real projects share one backend
// and database for the whole run (workers: 1, web/playwright.config.ts), so
// an approval left pending would count toward every later test's pending
// approvals list, in this file or another.
export async function denyPendingApproval(page: Page, sessionId: string): Promise<void> {
  const id = await approvalIdForSession(page, sessionId)
  const res = await page.request.post(`/api/v1/approvals/${id}`, { headers: HEADERS, data: { decision: 'deny' } })
  await ok(res, 'POST /approvals/{id} deny')
}

// setClaudeTokenPresence puts the current user's Claude token into the
// given presence state and makes sure the already-loaded page observes it,
// by a different mechanism per mode:
//
//   - Mock: pokes TanStack Query's ['me'] cache in place via
//     window.__queryClient (web/src/app.tsx only defines this under
//     VITE_MOCK=1). A fresh page.goto() would re-evaluate the mock handlers
//     module and reset its seeded state (the token back to present) right
//     along with it, so this has to happen without one - the mock's own
//     POST /__mock/reset-claude-token control route, called from inside the
//     page since msw's service worker only intercepts fetches the page's
//     own JS makes, not Playwright's Node-side page.request client (see
//     e2e/login.spec.ts's note on the same constraint for its logout
//     toggle).
//   - Real: window.__queryClient does not exist (production build, no
//     VITE_MOCK), so there is nothing to poke in place; a reload is used
//     instead. That's safe here (unlike the mock's module-reset problem
//     above) because the real token's presence lives server-side - a
//     reload just re-fetches the account's now-current state from the
//     database instead of the browser's stale cache.
export async function setClaudeTokenPresence(page: Page, testInfo: TestInfo, present: boolean): Promise<void> {
  if (!isReal(testInfo)) {
    if (!present) {
      const ok = await page.evaluate(async () => (await fetch('/__mock/reset-claude-token', { method: 'POST' })).ok)
      if (!ok) throw new Error('seed: /__mock/reset-claude-token failed')
    }
    // The mock seeds a present token by default; nothing to do to restore it
    // within one test (each test gets a fresh page, which re-seeds the mock
    // module).
    await page.evaluate(async () => {
      const client = (window as unknown as { __queryClient?: { refetchQueries: (f: { queryKey: string[] }) => Promise<unknown> } })
        .__queryClient
      await client?.refetchQueries({ queryKey: ['me'] })
    })
    return
  }
  if (present) {
    await ensureRealToken(page)
  } else {
    const res = await page.request.delete('/api/v1/me/claude-token', { headers: HEADERS })
    await ok(res, 'DELETE /me/claude-token')
  }
  await page.reload()
}
