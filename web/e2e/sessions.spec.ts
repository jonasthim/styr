import { test, expect, type Page } from '@playwright/test'
import {
  denyPendingApproval,
  ensureRealToken,
  ensureRealWorkspace,
  isReal,
  seedClosedSession,
  seedToolSession,
  seedWaitingSession,
} from './helpers/seed'

// Runs against both backends. Mock: /api/v1/me starts logged in as the dev
// admin; the mock seeds sessions in every state (waiting, running, closed,
// failed) but none in `open`, so the "Idle" group is absent until a new
// session is created (POST /api/v1/sessions always returns an `open`
// session). Real: a fresh backend starts with none of that, so each test
// seeds what it needs (e2e/helpers/seed.ts). The real projects run with
// workers: 1 (web/playwright.config.ts), so this file's tests never race
// each other or another file's over the shared account.

async function gotoReady(page: Page, path: string, heading: string) {
  await page.goto(path)
  await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  await page.waitForTimeout(50)
}

test.describe.configure({ mode: 'serial' })

test('create a session via the palette shortcut, then find it back in the list', async ({ page }, testInfo) => {
  // The New session dialog defaults its workspace select to the first
  // workspace the account has (NewSessionDialog.tsx); a fresh real backend
  // starts with none.
  if (isReal(testInfo)) {
    await ensureRealToken(page)
    await ensureRealWorkspace(page)
  }
  await gotoReady(page, '/sessions', 'Sessions')

  await page.keyboard.press('n')
  await expect(page).toHaveURL(/\/sessions\?new=1$/)
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()

  await dialog.getByLabel('Prompt').fill('hello')
  await page.keyboard.press('Control+Enter')

  await expect(page).toHaveURL(/\/sessions\/[0-9a-f-]{36}$/)
  const id = page.url().split('/').pop()!

  await page.getByRole('link', { name: 'Sessions' }).first().click()
  await expect(page.getByRole('heading', { name: 'Sessions' })).toBeVisible()

  const newRow = page.locator(`a[data-testid="session-row"][href$="${id}"]`)
  await expect(newRow).toContainText('Untitled session')
})

test('a seeded closed session row shows a cost like $0.05', async ({ page }, testInfo) => {
  if (isReal(testInfo)) await seedClosedSession(page, testInfo)
  await gotoReady(page, '/sessions', 'Sessions')
  const closedGroup = page.getByTestId('session-group-closed')
  await expect(closedGroup).toBeVisible()
  await expect(closedGroup.getByText(/\$\d+\.\d\d/).first()).toBeVisible()
})

test('group headers only render for groups that have sessions', async ({ page }, testInfo) => {
  let waitingSessionId: string | undefined
  if (isReal(testInfo)) {
    // Self-contained: seeds one session per group this test checks, rather
    // than relying on this file's earlier tests (or another file's) having
    // left the right states behind. `running` is deliberately not seeded:
    // the shell fake (testdata/fake-claude/fake-claude.sh) replays a
    // fixture in a few milliseconds, so a session only passes through
    // `running` for an instant - too short a window for a real HTTP+browser
    // round trip to reliably observe, unlike the mock's frozen fixture
    // data. The "Running" heading assertion below is skipped in real mode
    // for the same reason.
    await Promise.all([seedToolSession(page, testInfo), seedClosedSession(page, testInfo)])
    waitingSessionId = (await seedWaitingSession(page, testInfo)).sessionId
  }
  await gotoReady(page, '/sessions', 'Sessions')

  await expect(page.getByRole('heading', { name: 'Needs you' })).toBeVisible()
  if (!isReal(testInfo)) {
    await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  }
  await expect(page.getByRole('heading', { name: 'Closed' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Idle' })).toBeVisible()
  // Every rendered group header must have at least one row under it.
  for (const key of ['waiting', 'running', 'open', 'closed']) {
    const group = page.getByTestId(`session-group-${key}`)
    if ((await group.count()) === 0) continue
    await expect(group.getByRole('link').first()).toBeVisible()
  }

  if (isReal(testInfo) && waitingSessionId) await denyPendingApproval(page, waitingSessionId)
})

test('at 390 px the sessions list has no horizontal scroll', async ({ page }, testInfo) => {
  test.skip(!testInfo.project.name.includes('phone'), 'checks the phone viewport specifically')
  await gotoReady(page, '/sessions', 'Sessions')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
})

// First run: a server with no workspaces registered yet. POST
// /__mock/clear-workspaces and /__mock/set-role (src/mocks/handlers.ts) flip
// the mock in place and the caches are refetched, rather than reloading the
// page - a reload would re-evaluate the handlers module and re-seed both (the
// same reason e2e/profile.spec.ts resets the Claude token this way).
interface MockWindow {
  __queryClient: { refetchQueries: (f: { queryKey: string[] }) => Promise<unknown> }
}

async function firstRun(page: Page, role: 'admin' | 'member') {
  await page.evaluate(async (r) => {
    const cleared = await fetch('/__mock/clear-workspaces', { method: 'POST' })
    if (!cleared.ok) throw new Error(`clear-workspaces failed: ${cleared.status}`)
    const roleSet = await fetch('/__mock/set-role', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role: r }),
    })
    if (!roleSet.ok) throw new Error(`set-role failed: ${roleSet.status}`)
    const client = (window as unknown as MockWindow).__queryClient
    await client.refetchQueries({ queryKey: ['workspaces'] })
    await client.refetchQueries({ queryKey: ['me'] })
  }, role)
}

test('an admin with no workspaces gets a link to add one and cannot start', async ({ page }, testInfo) => {
  // Mock only: emptying the real backend's workspace table would break the
  // other real specs, which share one account and one serial run.
  test.skip(isReal(testInfo), 'drives the mock backend in place')
  await gotoReady(page, '/sessions', 'Sessions')
  await firstRun(page, 'admin')

  await page.getByRole('button', { name: 'New session' }).first().click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('no-workspaces-notice')).toBeVisible()
  await expect(dialog.getByText('No workspaces yet')).toBeVisible()
  await expect(dialog.getByRole('link', { name: 'Add a workspace' })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Start session' })).toBeDisabled()
})

test('a member with no workspaces gets a link to add one too', async ({ page }, testInfo) => {
  // Per-user workspaces (T29): a member can add a git or empty workspace
  // without an admin, so the empty-state notice no longer singles out
  // admins the way it did when only an admin could register one.
  test.skip(isReal(testInfo), 'drives the mock backend in place')
  await gotoReady(page, '/sessions', 'Sessions')
  await firstRun(page, 'member')

  await page.getByRole('button', { name: 'New session' }).first().click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('no-workspaces-notice')).toBeVisible()
  await expect(dialog.getByRole('link', { name: 'Add a workspace' })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Start session' })).toBeDisabled()
})

test('new session dialog closes and clears ?new=1', async ({ page }) => {
  await gotoReady(page, '/sessions', 'Sessions')
  await page.keyboard.press('n')
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page).toHaveURL(/\/sessions$/)
})
