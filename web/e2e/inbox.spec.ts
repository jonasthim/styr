import { test, expect, type Page } from '@playwright/test'

// Runs against the mock backend (msw handlers + FakeEventSource; see
// web/src/mocks/). /api/v1/me starts logged in as the dev admin. The mock
// seeds two pending approvals against the same session (web/src/mocks/handlers.ts):
// an "exec" tier Bash restart command (older, so it sorts first) and a
// "destructive" tier Bash rm command.

async function gotoInbox(page: Page) {
  await page.goto('/inbox')
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
  // Give the page-local keydown listener a moment to attach (see
  // e2e/shell.spec.ts's gotoReady for the same rationale).
  await page.waitForTimeout(50)
}

test('lists a pending Bash approval with an exec risk badge', async ({ page }) => {
  await gotoInbox(page)
  const cards = page.getByTestId('needs-you-list').getByRole('listitem')
  await expect(cards).toHaveCount(2)
  const first = cards.first()
  await expect(first).toContainText('Bash')
  await expect(first.getByTestId('risk-badge')).toHaveText('exec')
})

test('pressing a allows the focused card and removes it', async ({ page }) => {
  await gotoInbox(page)
  const cards = page.getByTestId('needs-you-list').getByRole('listitem')
  await expect(cards).toHaveCount(2)

  await page.keyboard.press('a')

  await expect(cards).toHaveCount(1)
  await expect(page.getByTestId('risk-badge')).toHaveText('destructive')
})

test('deciding every approval shows the empty state', async ({ page }) => {
  await gotoInbox(page)
  const cards = page.getByTestId('needs-you-list').getByRole('listitem')

  await page.keyboard.press('a')
  await expect(cards).toHaveCount(1)
  await page.keyboard.press('a')

  await expect(page.getByTestId('empty-inbox')).toBeVisible()
  await expect(page.getByText('Nothing needs you')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Start a session' })).toBeVisible()
})

test('the FYI section lists a recently closed session', async ({ page }) => {
  await gotoInbox(page)
  await expect(page.getByRole('heading', { name: 'FYI' })).toBeVisible()
  await expect(page.getByText('Summarize changelog')).toBeVisible()
})

test('phone layout renders approval action buttons at least 44px tall', async ({ page }, testInfo) => {
  test.skip(!testInfo.project.name.includes('phone'), 'phone-only assertion')
  await gotoInbox(page)

  const allow = page.getByRole('button', { name: 'Allow' }).first()
  await expect(allow).toBeVisible()
  const box = await allow.boundingBox()
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44)

  const deny = page.getByRole('button', { name: 'Deny' }).first()
  const denyBox = await deny.boundingBox()
  expect(denyBox?.height ?? 0).toBeGreaterThanOrEqual(44)
})
