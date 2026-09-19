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
import { execFileSync } from 'node:child_process'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
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

// The dev Codex verifier (cmd/styr/wire.go's devCodexVerifier) accepts
// anything shaped like an OpenAI API key ("sk-" prefix) without spawning a
// process, exactly as the Claude one does for "sk-ant-".
const REAL_CODEX_KEY = 'sk-proj-e2e-codex-key'

// ensureRealCodexKey (idempotent) makes sure the dev user has a working Codex
// API key, required before the real backend accepts a session on the codex
// harness (internal/sessions/service.go's Create fails fast without one).
export async function ensureRealCodexKey(page: Page): Promise<void> {
  const res = await page.request.put('/api/v1/me/codex-key', { headers: HEADERS, data: { key: REAL_CODEX_KEY } })
  await ok(res, 'PUT /me/codex-key')
}

// clearRealCodexKey (idempotent) removes the dev user's Codex key so a spec
// can start from the card's absent state. Real mode only: the mock seeds the
// key absent and re-seeds it on every fresh page, so there is nothing to
// clear there.
export async function clearRealCodexKey(page: Page, testInfo: TestInfo): Promise<void> {
  if (!isReal(testInfo)) return
  const res = await page.request.delete('/api/v1/me/codex-key', { headers: HEADERS })
  await ok(res, 'DELETE /me/codex-key')
  await page.reload()
}

// Cached for the lifetime of the worker process (the real projects run with
// workers: 1 - see playwright.config.ts - specifically so this in-memory
// cache, and every other cross-test assumption about a single shared real
// backend, stays valid without a database round trip per test).
let realWorkspaceId: string | null = null

// ensureRealWorkspace (idempotent) returns a workspace pointing at this
// repository's root (validated server-side by handleWorkspacesCreate: must
// exist, be a directory, contain .git), creating it once per run. Source
// "path" (T29's per-user workspaces contract) is the admin-only escape hatch
// for a checkout that already exists on the machine - exactly what this repo
// root is from the running backend's point of view - and the dev account
// used throughout the suite is always the admin.
export const REAL_WORKSPACE_NAME = 'styr-e2e'

export async function ensureRealWorkspace(page: Page): Promise<string> {
  if (realWorkspaceId) return realWorkspaceId
  const repoRoot = path.resolve(process.cwd(), '..')
  const res = await page.request.post('/api/v1/workspaces', {
    headers: HEADERS,
    data: { name: REAL_WORKSPACE_NAME, source: 'path', path: repoRoot, default_profile_id: REAL_PROFILE_ID },
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
  const existing = workspaces.find((w) => w.name === REAL_WORKSPACE_NAME)
  if (!existing) throw new Error(`seed: could not create or find the e2e workspace: ${res.status()} ${await res.text()}`)
  realWorkspaceId = existing.id
  return realWorkspaceId
}

// --- triggers, templates and runs (v0.2) ----------------------------------

// The service-wide Claude token an unattended run needs: a run's session is
// created with no owner (internal/runs/runs.go), so sessions.Service resolves
// its token from the server-wide setting, not from the dev user's own. Without
// it every delivery is logged as "failed: add a service token in settings".
export async function ensureRealServiceToken(page: Page): Promise<void> {
  const res = await page.request.put('/api/v1/settings/service-token', {
    headers: HEADERS,
    data: { token: 'sk-ant-oat01-e2e-service-token' },
  })
  await ok(res, 'PUT /settings/service-token')
}

// The shared Grafana template's name, matching what a fresh install seeds
// (internal/templates/seed.go).
export const GRAFANA_TEMPLATE_NAME = 'Grafana alert investigation'

// The prompt the real-mode template renders. It is the seeded Grafana prompt
// with fake-claude.sh's "[fixture:06]" marker in front, so the run's first
// (and only) turn replays internal/harness/claude/testdata/06_json_schema.jsonl
// - the recording of a --json-schema session, whose result carries a
// structured_output the run engine turns into the run's report.
const GRAFANA_PROMPT_TEMPLATE = `[fixture:06] A Grafana alert is {{ .status }}. Investigate read-only and report.
{{ range .alerts }}- {{ .labels.alertname }} on {{ default "unknown" .labels.instance }}: {{ .annotations.summary }}
  {{ .annotations.description }} (since {{ .startsAt }})
{{ end }}
Use the workspace's runbooks and only read-only commands. Do not change anything.`

const GRAFANA_REPORT_SCHEMA = JSON.stringify({
  type: 'object',
  required: ['severity', 'diagnosis', 'proposed_action', 'confidence'],
  properties: {
    severity: { enum: ['info', 'warning', 'critical'] },
    diagnosis: { type: 'string' },
    evidence: { type: 'array', items: { type: 'string' } },
    proposed_action: { type: 'string' },
    confidence: { type: 'number', minimum: 0, maximum: 1 },
    resolved_itself: { type: 'boolean' },
  },
})

interface MinimalTemplate {
  id: string
  name: string
}

let realTemplateId: string | null = null

// ensureRealGrafanaTemplate (idempotent) returns the shared "Grafana alert
// investigation" template, creating it when it is missing.
//
// A fresh install seeds it at serve start (cmd/styr/serve.go's seedTemplates),
// but only bound to a shared workspace that already exists then - and this
// suite registers its own shared workspace long after the server booted, so
// nothing is seeded and the template has to be created here. When one does
// exist (an operator's, or a future boot-time seed), it is updated in place
// instead, so its prompt always carries the fixture marker.
export async function ensureRealGrafanaTemplate(page: Page): Promise<MinimalTemplate> {
  if (realTemplateId) return { id: realTemplateId, name: GRAFANA_TEMPLATE_NAME }
  const workspaceId = await ensureRealWorkspace(page)
  const body = {
    name: GRAFANA_TEMPLATE_NAME,
    workspace_id: workspaceId,
    profile_id: 'investigate',
    title_template: '{{ .status }}: {{ join ", " (alertnames .alerts) }}',
    prompt_template: GRAFANA_PROMPT_TEMPLATE,
    system_prompt: 'You are investigating a production alert read-only. Never change state.',
    report_schema: GRAFANA_REPORT_SCHEMA,
    shared: true,
  }

  const list = await page.request.get('/api/v1/templates', { headers: HEADERS })
  await ok(list, 'GET /templates')
  const existing = ((await list.json()) as MinimalTemplate[]).find((t) => t.name === GRAFANA_TEMPLATE_NAME)
  if (existing) {
    const patch = await page.request.patch(`/api/v1/templates/${existing.id}`, { headers: HEADERS, data: body })
    await ok(patch, 'PATCH /templates/{id}')
    realTemplateId = existing.id
    return { id: existing.id, name: GRAFANA_TEMPLATE_NAME }
  }

  const res = await page.request.post('/api/v1/templates', { headers: HEADERS, data: body })
  await ok(res, 'POST /templates')
  const created = (await res.json()) as MinimalTemplate
  realTemplateId = created.id
  return created
}

// grafanaSamplePayload returns the backend's own Grafana example body
// (GET /triggers/samples/grafana), the same one the "Send test payload"
// dialog prefills, so a delivery test posts exactly what the UI would.
export async function grafanaSamplePayload(page: Page): Promise<Record<string, unknown>> {
  const res = await page.request.get('/api/v1/triggers/samples/grafana', { headers: HEADERS })
  await ok(res, 'GET /triggers/samples/grafana')
  return (await res.json()) as Record<string, unknown>
}

export interface RealTrigger {
  id: string
  name: string
  slug: string
  secret: string
}

// createRealTrigger creates a grafana trigger bound to the shared template
// over the API, for specs that need a trigger but are not themselves
// exercising the create dialog (e2e/triggers.spec.ts drives that through the
// UI and captures the secret from the "Webhook ready" panel).
export async function createRealTrigger(page: Page, name: string): Promise<RealTrigger> {
  await ensureRealServiceToken(page)
  const template = await ensureRealGrafanaTemplate(page)
  const res = await page.request.post('/api/v1/triggers', {
    headers: HEADERS,
    data: { name, kind: 'grafana', template_id: template.id, cooldown_s: 600, storm_cap_per_hour: 10 },
  })
  await ok(res, 'POST /triggers')
  const body = (await res.json()) as { trigger: { id: string; name: string; slug: string }; secret: string }
  return { ...body.trigger, secret: body.secret }
}

interface MinimalRunView {
  run: { id: string; outcome: string; report: unknown; summary: string }
}

// waitForRunOutcome polls GET /runs/{id} until the run leaves "running".
// The run engine closes a run out from the session's result event, which the
// shell fake produces within a second or two; the generous default leaves
// room for a loaded machine without masking a stuck pipeline.
export async function waitForRunOutcome(page: Page, runId: string, timeoutMs = 30_000): Promise<MinimalRunView> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const res = await page.request.get(`/api/v1/runs/${runId}`, { headers: HEADERS })
    await ok(res, `GET /runs/${runId}`)
    const view = (await res.json()) as MinimalRunView
    if (view.run.outcome !== 'running') return view
    if (Date.now() > deadline) throw new Error(`seed: run ${runId} was still running after ${timeoutMs}ms`)
    await new Promise((r) => setTimeout(r, 200))
  }
}

let realSuccessRunId: string | null = null

// ensureRealSuccessRun (idempotent) guarantees the real backend has at least
// one finished, successful run - what e2e/runs.spec.ts's filter chips need to
// assert against. It forces a delivery through POST /triggers/{id}/test
// (force: true skips dedupe, cooldown and the storm cap) rather than the
// inbound hook, so it never competes with triggers.spec.ts's own deliveries.
export async function ensureRealSuccessRun(page: Page): Promise<string> {
  if (realSuccessRunId) return realSuccessRunId
  const trigger = await createRealTrigger(page, `Runs list source ${Date.now()}`)
  const res = await page.request.post(`/api/v1/triggers/${trigger.id}/test`, {
    headers: HEADERS,
    data: { payload: await grafanaSamplePayload(page), force: true },
  })
  await ok(res, 'POST /triggers/{id}/test')
  const result = (await res.json()) as { status: string; run_id?: string }
  if (!result.run_id) throw new Error(`seed: test delivery did not start a run (status ${result.status})`)
  const view = await waitForRunOutcome(page, result.run_id)
  if (view.run.outcome !== 'success') throw new Error(`seed: run ${result.run_id} finished as ${view.run.outcome}`)
  realSuccessRunId = result.run_id
  return realSuccessRunId
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

export interface CodexSession {
  workspaceId: string
  sessionId: string
}

// seedCodexSession: a session on the codex harness whose single turn is the
// Codex fixture 01 (internal/harness/codex/testdata/01_simple_text.jsonl: the
// text "pong", then a completed turn), in `open` state. Real mode only - the
// mock has no server-side session store, so a mock-mode Codex session is
// created through the new-session dialog instead (e2e/session.spec.ts).
export async function seedCodexSession(page: Page, title: string): Promise<CodexSession> {
  await ensureRealCodexKey(page)
  const workspaceId = await ensureRealWorkspace(page)
  const res = await page.request.post('/api/v1/sessions', {
    headers: HEADERS,
    data: { workspace_id: workspaceId, profile_id: REAL_PROFILE_ID, title, prompt: '[fixture:01] hi', harness: 'codex' },
  })
  await ok(res, 'POST /sessions (codex)')
  const sess = (await res.json()) as MinimalSession
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

// --- review: a worktree-enabled workspace on a throwaway repo (v0.3) ------

// The file the review session's prompt makes the shell fake write into the
// session's worktree, and the content it writes (three lines, no trailing
// newline). Exported so the spec asserts against one definition.
export const WORKTREE_FILE = 'src/hello.go'

// fake-claude.sh's two markers in one prompt (see its header comment):
// "[write:<path>:<text>]" writes the file into the fake's cwd - the session's
// own worktree - before the turn replays, and "[fixture:02]" replays the Read
// fixture so the turn looks like ordinary work.
//
// The newlines below are real newlines, not the two-character escape: the
// fake matches the marker against the raw JSON line it reads on stdin, where
// JSON encoding has already turned each newline into a literal backslash-n -
// which is exactly the sequence the fake expands back into a newline before
// writing the file. Writing "\\n" here would reach it doubled and land a
// stray backslash in the file. The text may not contain "]".
const WORKTREE_PROMPT = '[write:src/hello.go:package main\n\nfunc main() {}][fixture:02] change'

/** Creates a git repository with one commit under a fresh temp directory and
 * returns its path. Deliberately without a remote: opening a pull request
 * from a session on it answers 409 no_remote, which is the state the review
 * spec asserts is reported cleanly. */
function createWorktreeRepo(): string {
  const dir = mkdtempSync(path.join(tmpdir(), 'styr-e2e-worktrees-'))
  execFileSync('git', ['init', '-q', '-b', 'main', dir])
  writeFileSync(path.join(dir, 'README.md'), '# styr review fixture\n')
  execFileSync('git', ['-C', dir, 'add', 'README.md'])
  execFileSync('git', [
    '-C',
    dir,
    '-c',
    'user.email=e2e@styr.local',
    '-c',
    'user.name=styr-e2e',
    'commit',
    '-q',
    '-m',
    'seed',
  ])
  return dir
}

let realWorktreeWorkspace: { id: string; name: string } | null = null

// ensureRealWorktreeWorkspaceRef (idempotent, cached for the worker)
// registers the throwaway repo above as a "path" workspace with worktrees
// and auto-checkpointing on, so every session created in it runs in its own
// git worktree and leaves a checkpoint commit after each turn. Source "path"
// is admin-only, and the dev account the suite runs as is the admin.
//
// The name comes back as well as the id because a pipeline definition names
// its workspace by name, not by id, and the validator rejects a mismatch
// (internal/pipelines' Executor.Validate) - and this one's name carries a
// timestamp, so it cannot be a constant.
export async function ensureRealWorktreeWorkspaceRef(page: Page): Promise<{ id: string; name: string }> {
  if (realWorktreeWorkspace) return realWorktreeWorkspace
  const name = `styr-e2e-worktrees-${Date.now()}`
  const res = await page.request.post('/api/v1/workspaces', {
    headers: HEADERS,
    data: {
      name,
      source: 'path',
      path: createWorktreeRepo(),
      default_profile_id: REAL_PROFILE_ID,
      worktrees: true,
      auto_checkpoint: true,
    },
  })
  await ok(res, 'POST /workspaces (worktrees)')
  const ws = (await res.json()) as MinimalWorkspace
  realWorktreeWorkspace = { id: ws.id, name }
  return realWorktreeWorkspace
}

export async function ensureRealWorktreeWorkspace(page: Page): Promise<string> {
  return (await ensureRealWorktreeWorkspaceRef(page)).id
}

export interface WorktreeSession {
  workspaceId: string
  sessionId: string
}

// seedWorktreeSession: a session in the worktree workspace whose first turn
// writes WORKTREE_FILE into its worktree and then replays fixture 02, so the
// session is `open` with a real one-file diff behind it. Each call gets its
// own session (and worktree): the review actions a spec drives - commit,
// rewind, discard - are one-way, so tests must not share one.
export async function seedWorktreeSession(page: Page, title: string): Promise<WorktreeSession> {
  await ensureRealToken(page)
  const workspaceId = await ensureRealWorktreeWorkspace(page)
  const res = await page.request.post('/api/v1/sessions', {
    headers: HEADERS,
    data: { workspace_id: workspaceId, profile_id: REAL_PROFILE_ID, title, prompt: WORKTREE_PROMPT },
  })
  await ok(res, 'POST /sessions (worktree)')
  const sess = (await res.json()) as MinimalSession
  await waitForSessionState(page, sess.id, ['open'])
  return { workspaceId, sessionId: sess.id }
}

// --- plan approval (v0.3) --------------------------------------------------

const PLAN_PROFILE_NAME = 'styr-e2e-plan'

let realPlanProfileId: string | null = null

// ensureRealPlanProfile (idempotent) returns a custom profile with permission
// mode "plan" - one of the modes internal/api's allowedModes permits, unlike
// the CLI's skip-all-permissions mode. A session started under it ends its
// first turn with an ExitPlanMode permission request instead of a result.
export async function ensureRealPlanProfile(page: Page): Promise<string> {
  if (realPlanProfileId) return realPlanProfileId
  const list = await page.request.get('/api/v1/profiles', { headers: HEADERS })
  await ok(list, 'GET /profiles')
  const existing = ((await list.json()) as Array<{ id: string; name: string }>).find((p) => p.name === PLAN_PROFILE_NAME)
  if (existing) {
    realPlanProfileId = existing.id
    return realPlanProfileId
  }
  const res = await page.request.post('/api/v1/profiles', {
    headers: HEADERS,
    data: { name: PLAN_PROFILE_NAME, mode: 'plan' },
  })
  await ok(res, 'POST /profiles')
  const created = (await res.json()) as { id: string }
  realPlanProfileId = created.id
  return realPlanProfileId
}

// seedPlanSession: a plan-mode session waiting on its ExitPlanMode request,
// replaying fixture 08 (internal/harness/claude/testdata/08_plan_mode.jsonl -
// the recorded `--permission-mode plan` run whose plan is about adding a
// --version flag). It waits on the ordinary repo workspace, not the worktree
// one: what is under test is the plan card, not the diff.
export async function seedPlanSession(page: Page, title: string): Promise<string> {
  await ensureRealToken(page)
  const workspaceId = await ensureRealWorkspace(page)
  const profileId = await ensureRealPlanProfile(page)
  const res = await page.request.post('/api/v1/sessions', {
    headers: HEADERS,
    data: { workspace_id: workspaceId, profile_id: profileId, title, prompt: '[fixture:08] plan it' },
  })
  await ok(res, 'POST /sessions (plan)')
  const sess = (await res.json()) as MinimalSession
  await waitForSessionState(page, sess.id, ['waiting'])
  return sess.id
}

// --- schedules, loops and stats (v0.4) -------------------------------------

// Both templates below render no variables at all, unlike the Grafana one: a
// schedule fires with only its own `vars` plus the `schedule` key, and a loop
// re-renders the same prompt every iteration, so a prompt that needed a
// webhook payload would render empty either way. What matters is the
// "[fixture:06]" marker, which makes every turn replay
// internal/harness/claude/testdata/06_json_schema.jsonl - a recorded
// --json-schema session whose result carries a structured_output the run
// engine stores as the run's report.
const FIXTURE_06_PROMPT = '[fixture:06] The disk on host x is 91 percent full. Give your diagnosis.'

// Fixture 06's structured_output has severity, diagnosis and confidence -
// and no `done` field, which is what makes the loop template below iterate
// until it runs out of budget rather than finishing on its first report.
const FIXTURE_06_REPORT_SCHEMA = JSON.stringify({
  type: 'object',
  required: ['severity', 'diagnosis'],
  properties: {
    severity: { enum: ['info', 'warning', 'critical'] },
    diagnosis: { type: 'string' },
    confidence: { type: 'number', minimum: 0, maximum: 1 },
    done: { type: 'boolean' },
  },
})

export const SCHEDULE_TEMPLATE_NAME = 'Disk check (scheduled)'
export const LOOP_TEMPLATE_NAME = 'Disk check until done'

interface TemplateBody {
  name: string
  workspace_id: string
  profile_id: string
  title_template: string
  prompt_template: string
  system_prompt: string
  report_schema: string
  loop_until: string
  loop_max: number
  shared: boolean
}

/** Creates the named shared template, or updates the existing one in place
 * so its prompt always carries the fixture marker - the same idempotent
 * shape ensureRealGrafanaTemplate uses, and for the same reason: the real
 * projects share one backend and database for the whole run. */
async function ensureRealTemplate(page: Page, body: TemplateBody): Promise<string> {
  const list = await page.request.get('/api/v1/templates', { headers: HEADERS })
  await ok(list, 'GET /templates')
  const existing = ((await list.json()) as MinimalTemplate[]).find((t) => t.name === body.name)
  if (existing) {
    const patch = await page.request.patch(`/api/v1/templates/${existing.id}`, { headers: HEADERS, data: body })
    await ok(patch, 'PATCH /templates/{id}')
    return existing.id
  }
  const res = await page.request.post('/api/v1/templates', { headers: HEADERS, data: body })
  await ok(res, 'POST /templates')
  return ((await res.json()) as MinimalTemplate).id
}

let realScheduleTemplateId: string | null = null

// ensureRealScheduleTemplate (idempotent): the one-shot template a real-mode
// schedule fires. A schedule's run is unattended - its session has no owner -
// so the service token has to be in place before it can start.
export async function ensureRealScheduleTemplate(page: Page): Promise<string> {
  if (realScheduleTemplateId) return realScheduleTemplateId
  await ensureRealServiceToken(page)
  realScheduleTemplateId = await ensureRealTemplate(page, {
    name: SCHEDULE_TEMPLATE_NAME,
    workspace_id: await ensureRealWorkspace(page),
    profile_id: 'investigate',
    title_template: 'Disk check',
    prompt_template: FIXTURE_06_PROMPT,
    system_prompt: 'You are checking a host read-only. Never change state.',
    report_schema: FIXTURE_06_REPORT_SCHEMA,
    loop_until: '',
    loop_max: 0,
    shared: true,
  })
  return realScheduleTemplateId
}

let realLoopTemplateId: string | null = null

// ensureRealLoopTemplate (idempotent): the same prompt with a loop on it.
// `done` is a field fixture 06's report never carries, so every iteration
// reads as "keep going" and the loop ends on its budget - state `exhausted`
// at iteration 2 of 2 - rather than on a report that says it is finished.
export async function ensureRealLoopTemplate(page: Page): Promise<string> {
  if (realLoopTemplateId) return realLoopTemplateId
  await ensureRealServiceToken(page)
  realLoopTemplateId = await ensureRealTemplate(page, {
    name: LOOP_TEMPLATE_NAME,
    workspace_id: await ensureRealWorkspace(page),
    profile_id: 'investigate',
    title_template: 'Disk check until done',
    prompt_template: FIXTURE_06_PROMPT,
    system_prompt: 'You are checking a host read-only. Never change state.',
    report_schema: FIXTURE_06_REPORT_SCHEMA,
    loop_until: 'done',
    loop_max: 2,
    shared: true,
  })
  return realLoopTemplateId
}

// --- pipelines (v0.5) ------------------------------------------------------
// A pipeline step is an unattended run of a template, so everything below is
// built out of the same two pieces the v0.4 helpers use: a shared template
// whose prompt carries a fake-claude.sh marker, and the service token those
// runs need. What is new is that a pipeline definition names its workspace
// by *name* and its steps' templates by name, and the validator rejects a
// definition whose workspace does not match or whose template does not exist
// in it (internal/pipelines' Executor.Validate) - so the names below are the
// contract between these helpers and the yaml the specs write.

/** Fixture 06's structured_output carries this phrase in `diagnosis`. A step
 * that renders an earlier step's diagnosis into its own prompt puts it in
 * that step's transcript, which is how the real-mode spec proves the report
 * really flowed from one agent to the next. */
export const FIXTURE_06_DIAGNOSIS_PHRASE = 'above the alert threshold'

/** Replays fixture 06: a structured report with severity, diagnosis and
 * confidence. */
export const PIPELINE_REPORT_TEMPLATE_NAME = 'Pipeline step (reports)'
/** Renders `.note` - what a `with:` entry feeds it - into its own prompt. */
export const PIPELINE_NOTE_TEMPLATE_NAME = 'Pipeline step (reads a note)'
/** Replays fixture 05, whose result is an interrupted turn flagged
 * is_error, so the run behind the step finishes `failed`. */
export const PIPELINE_FAILING_TEMPLATE_NAME = 'Pipeline step (fails)'
/** The two worktree steps: each writes one file into the worktree its
 * session runs in, so a `worktree: shared` pair leaves both files in one. */
export const PIPELINE_WRITE_ONE_TEMPLATE_NAME = 'Pipeline step (writes step1)'
export const PIPELINE_WRITE_TWO_TEMPLATE_NAME = 'Pipeline step (writes step2)'

/** The files those two steps write, relative to their worktree. */
export const PIPELINE_WORKTREE_FILE_ONE = 'notes/step1.txt'
export const PIPELINE_WORKTREE_FILE_TWO = 'notes/step2.txt'

let realPipelineTemplates: Record<string, string> | null = null

/** ensureRealPipelineTemplates (idempotent): the three templates the
 * repo-workspace pipelines below are built from, keyed by name. */
export async function ensureRealPipelineTemplates(page: Page): Promise<Record<string, string>> {
  if (realPipelineTemplates) return realPipelineTemplates
  await ensureRealServiceToken(page)
  const workspaceId = await ensureRealWorkspace(page)
  const base = {
    workspace_id: workspaceId,
    profile_id: 'investigate',
    system_prompt: 'You are checking a host read-only. Never change state.',
    loop_until: '',
    loop_max: 0,
    shared: true,
  }
  const entries: Array<[string, TemplateBody]> = [
    [
      PIPELINE_REPORT_TEMPLATE_NAME,
      {
        ...base,
        name: PIPELINE_REPORT_TEMPLATE_NAME,
        title_template: 'Pipeline report step',
        prompt_template: FIXTURE_06_PROMPT,
        report_schema: FIXTURE_06_REPORT_SCHEMA,
      },
    ],
    [
      PIPELINE_NOTE_TEMPLATE_NAME,
      {
        ...base,
        name: PIPELINE_NOTE_TEMPLATE_NAME,
        title_template: 'Pipeline note step',
        // `.note` is whatever the step's `with:` rendered - for the two-step
        // pipeline below, the first step's own diagnosis.
        prompt_template: '[fixture:06] Earlier diagnosis: {{ .note }}',
        report_schema: FIXTURE_06_REPORT_SCHEMA,
      },
    ],
    [
      PIPELINE_FAILING_TEMPLATE_NAME,
      {
        ...base,
        name: PIPELINE_FAILING_TEMPLATE_NAME,
        title_template: 'Pipeline failing step',
        prompt_template: '[fixture:05] Count slowly from 1 to 300, one number per line, no other text.',
        report_schema: '',
      },
    ],
  ]
  const resolved: Record<string, string> = {}
  for (const [name, body] of entries) {
    resolved[name] = await ensureRealTemplate(page, body)
  }
  realPipelineTemplates = resolved
  return resolved
}

let realWorktreePipelineTemplates: Record<string, string> | null = null

/** ensureRealWorktreePipelineTemplates (idempotent): the two templates the
 * shared-worktree pipeline is built from, on the worktree-enabled
 * workspace. Each prompt carries fake-claude.sh's "[write:path:text]"
 * marker, which writes the file into the fake's cwd - the session's own
 * worktree - before the turn replays. */
export async function ensureRealWorktreePipelineTemplates(page: Page): Promise<Record<string, string>> {
  if (realWorktreePipelineTemplates) return realWorktreePipelineTemplates
  await ensureRealServiceToken(page)
  const { id: workspaceId } = await ensureRealWorktreeWorkspaceRef(page)
  const base = {
    workspace_id: workspaceId,
    profile_id: 'investigate',
    system_prompt: 'You are writing one note file and reporting.',
    report_schema: FIXTURE_06_REPORT_SCHEMA,
    loop_until: '',
    loop_max: 0,
    shared: true,
  }
  const resolved: Record<string, string> = {
    [PIPELINE_WRITE_ONE_TEMPLATE_NAME]: await ensureRealTemplate(page, {
      ...base,
      name: PIPELINE_WRITE_ONE_TEMPLATE_NAME,
      title_template: 'Write step1',
      prompt_template: `[write:${PIPELINE_WORKTREE_FILE_ONE}:hello][fixture:06] Write the first note.`,
    }),
    [PIPELINE_WRITE_TWO_TEMPLATE_NAME]: await ensureRealTemplate(page, {
      ...base,
      name: PIPELINE_WRITE_TWO_TEMPLATE_NAME,
      title_template: 'Write step2',
      prompt_template: `[write:${PIPELINE_WORKTREE_FILE_TWO}:world][fixture:06] Write the second note.`,
    }),
  }
  realWorktreePipelineTemplates = resolved
  return resolved
}

export interface RealPipeline {
  id: string
  name: string
}

/** Creates a shared pipeline over the API, for the specs that need one to
 * exist without driving the editor to make it. The definition is validated
 * server-side, so a 422 here means the yaml is wrong, not the test. */
export async function createRealPipeline(page: Page, name: string, workspaceId: string, yaml: string): Promise<RealPipeline> {
  const res = await page.request.post('/api/v1/pipelines', {
    headers: HEADERS,
    data: { name, workspace_id: workspaceId, yaml, shared: true },
  })
  await ok(res, 'POST /pipelines')
  const created = (await res.json()) as { id: string; name: string }
  return created
}

interface MinimalPipelineRunView {
  run: { id: string; state: string }
  steps: Array<{ step_id: string; state: string; attempt: number; run_id: string | null }>
}

/** Polls GET /pipeline-runs/{id} until the run leaves "running". A pipeline
 * step is a whole session start plus a fixture replay, and a fan-out or a
 * retry is several of them in a row, so the default budget is generous. */
export async function waitForPipelineRun(page: Page, id: string, timeoutMs = 90_000): Promise<MinimalPipelineRunView> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const res = await page.request.get(`/api/v1/pipeline-runs/${id}`, { headers: HEADERS })
    await ok(res, `GET /pipeline-runs/${id}`)
    const view = (await res.json()) as MinimalPipelineRunView
    if (view.run.state !== 'running') return view
    if (Date.now() > deadline) {
      throw new Error(`seed: pipeline run ${id} was still running after ${timeoutMs}ms`)
    }
    await new Promise((r) => setTimeout(r, 300))
  }
}
