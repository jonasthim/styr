import { test, expect, type Page } from '@playwright/test'

// Runs against the mock backend (msw handlers + FakeEventSource; see
// web/src/mocks/). /api/v1/me starts logged in as the dev admin. The mock
// seeds sessions in every state (waiting, running, closed, failed) but none
// in `open`, so the "Idle" group is absent until a new session is created
// (POST /api/v1/sessions always returns an `open` session).

async function gotoReady(page: Page, path: string, heading: string) {
  await page.goto(path)
  await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  await page.waitForTimeout(50)
}

test('create a session via the palette shortcut, then find it back in the list', async ({ page }) => {
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

test('a seeded closed session row shows a cost like $0.05', async ({ page }) => {
  await gotoReady(page, '/sessions', 'Sessions')
  const closedGroup = page.getByTestId('session-group-closed')
  await expect(closedGroup).toBeVisible()
  await expect(closedGroup.getByText(/\$\d+\.\d\d/).first()).toBeVisible()
})

test('group headers only render for groups that have sessions', async ({ page }) => {
  await gotoReady(page, '/sessions', 'Sessions')

  await expect(page.getByRole('heading', { name: 'Needs you' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Closed' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Idle' })).toBeVisible()
  // Every rendered group header must have at least one row under it.
  for (const key of ['waiting', 'running', 'open', 'closed']) {
    const group = page.getByTestId(`session-group-${key}`)
    if ((await group.count()) === 0) continue
    await expect(group.getByRole('link').first()).toBeVisible()
  }
})

test('at 390 px the sessions list has no horizontal scroll', async ({ page }, testInfo) => {
  test.skip(!testInfo.project.name.includes('phone'), 'checks the phone viewport specifically')
  await gotoReady(page, '/sessions', 'Sessions')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
})

test('new session dialog closes and clears ?new=1', async ({ page }) => {
  await gotoReady(page, '/sessions', 'Sessions')
  await page.keyboard.press('n')
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page).toHaveURL(/\/sessions$/)
})
