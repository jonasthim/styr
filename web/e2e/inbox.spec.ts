import { test, expect, type Page } from '@playwright/test'
import { denyPendingApproval, isReal, seedClosedSession, seedWaitingSession } from './helpers/seed'

// Runs against both backends. Mock: /api/v1/me starts logged in as the dev
// admin, and the mock seeds two pending approvals against the same session
// (web/src/mocks/handlers.ts): an "exec" tier Bash restart command (older,
// so it sorts first) and a "destructive" tier Bash rm command. Real: each
// test seeds its own session(s) (e2e/helpers/seed.ts) - a single "waiting"
// session carrying fixture 03's Bash permission request, whose risk tier is
// "exec" (internal/risk/risk.go classifies its `mkdir -p ...` command as
// exec, not read or destructive). The real projects run with workers: 1
// (web/playwright.config.ts), so this file's tests never race each other or
// another file's over the account's shared pending-approvals list.

async function gotoInbox(page: Page) {
  await page.goto('/inbox')
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
  // Give the page-local keydown listener a moment to attach (see
  // e2e/shell.spec.ts's gotoReady for the same rationale).
  await page.waitForTimeout(50)
}

test.describe.configure({ mode: 'serial' })

test('lists a pending Bash approval with an exec risk badge', async ({ page }, testInfo) => {
  const { sessionId } = await seedWaitingSession(page, testInfo)
  await gotoInbox(page)
  const cards = page.getByTestId('needs-you-list').getByRole('listitem')
  if (isReal(testInfo)) {
    await expect(cards).toHaveCount(1)
    await expect(cards.first()).toContainText('Bash')
    await expect(cards.first().getByTestId('risk-badge')).toHaveText('exec')
    await denyPendingApproval(page, sessionId)
  } else {
    await expect(cards).toHaveCount(2)
    const first = cards.first()
    await expect(first).toContainText('Bash')
    await expect(first.getByTestId('risk-badge')).toHaveText('exec')
  }
})

test('pressing a allows the focused card and removes it', async ({ page }, testInfo) => {
  const { sessionId } = await seedWaitingSession(page, testInfo)
  await gotoInbox(page)
  const cards = page.getByTestId('needs-you-list').getByRole('listitem')

  if (isReal(testInfo)) {
    await expect(cards).toHaveCount(1)
    await page.keyboard.press('a')
    await expect(cards).toHaveCount(0)
    await expect(page.getByTestId('empty-inbox')).toBeVisible()

    // The approval's own session leaves `waiting` once decided: fixture
    // 03's Bash call is the last tool use before its recorded result, so
    // the session returns to `open`.
    await expect
      .poll(
        async () => {
          const res = await page.request.get(`/api/v1/sessions/${sessionId}`, {
            headers: { 'X-Requested-With': 'styr' },
          })
          const body = (await res.json()) as { state: string }
          return body.state
        },
        { timeout: 20_000 },
      )
      .toBe('open')
  } else {
    await expect(cards).toHaveCount(2)
    await page.keyboard.press('a')
    await expect(cards).toHaveCount(1)
    await expect(page.getByTestId('risk-badge')).toHaveText('destructive')
  }
})

test('deciding every approval shows the empty state', async ({ page }, testInfo) => {
  await seedWaitingSession(page, testInfo)
  await gotoInbox(page)
  const cards = page.getByTestId('needs-you-list').getByRole('listitem')

  if (isReal(testInfo)) {
    await expect(cards).toHaveCount(1)
    await page.keyboard.press('a')
  } else {
    await page.keyboard.press('a')
    await expect(cards).toHaveCount(1)
    await page.keyboard.press('a')
  }

  await expect(page.getByTestId('empty-inbox')).toBeVisible()
  await expect(page.getByText('Nothing needs you')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Start a session' })).toBeVisible()
})

test('the FYI section lists a recently closed session', async ({ page }, testInfo) => {
  const { title } = await seedClosedSession(page, testInfo)
  await gotoInbox(page)
  await expect(page.getByRole('heading', { name: 'FYI' })).toBeVisible()
  await expect(page.getByText(title)).toBeVisible()
})

test('phone layout renders approval action buttons at least 44px tall', async ({ page }, testInfo) => {
  test.skip(!testInfo.project.name.endsWith('phone'), 'phone-only assertion')
  const { sessionId } = await seedWaitingSession(page, testInfo)
  await gotoInbox(page)

  const allow = page.getByRole('button', { name: 'Allow' }).first()
  await expect(allow).toBeVisible()
  const box = await allow.boundingBox()
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44)

  const deny = page.getByRole('button', { name: 'Deny' }).first()
  const denyBox = await deny.boundingBox()
  expect(denyBox?.height ?? 0).toBeGreaterThanOrEqual(44)

  if (isReal(testInfo)) await denyPendingApproval(page, sessionId)
})
