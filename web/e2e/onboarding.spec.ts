import { test, expect, type Page } from '@playwright/test'

// Runs against the mock backend (see e2e/login.spec.ts for the general mock
// setup notes). /api/v1/me starts logged in as the dev admin with a present
// Claude token, so /inbox loads normally by default; these tests flip the
// token to absent in place (POST /__mock/reset-claude-token,
// src/mocks/handlers.ts), the same pattern e2e/profile.spec.ts uses, since a
// fresh page.goto() would re-evaluate the mock handlers module and reset
// that flag right back.

interface MockWindow {
  __queryClient: { refetchQueries: (f: { queryKey: string[] }) => Promise<unknown> }
}

async function resetClaudeTokenAbsentAndRefetchMe(page: Page) {
  await page.evaluate(async () => {
    const res = await fetch('/__mock/reset-claude-token', { method: 'POST' })
    if (!res.ok) throw new Error(`reset-claude-token failed: ${res.status}`)
    await (window as unknown as MockWindow).__queryClient.refetchQueries({ queryKey: ['me'] })
  })
}

test('redirects /inbox to /welcome when the Claude token is absent and onboarding has not been seen, and Skip does not bounce back', async ({
  page,
}) => {
  await page.goto('/inbox')
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()

  // Starting state for a fresh browser context: no styr.welcomed flag yet.
  await page.evaluate(() => sessionStorage.removeItem('styr.welcomed'))
  await resetClaudeTokenAbsentAndRefetchMe(page)

  await expect(page).toHaveURL(/\/welcome$/)
  await expect(page.getByRole('heading', { name: 'Welcome to Styr' })).toBeVisible()

  // Keep the mouse away from the rail on the way to Skip: a click's path
  // crossing the left edge can trigger its hover-expand, which then
  // intercepts the pointer (see e2e/shell.spec.ts's "[ pins the rail open"
  // test for the same guard).
  await page.mouse.move(700, 400)
  await page.getByRole('button', { name: 'Skip' }).click()
  await expect(page).toHaveURL(/\/inbox$/)
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()

  // Leaving and returning to /inbox client-side, still with no Claude
  // token, must not bounce back to /welcome: Skip already recorded
  // styr.welcomed, and a fresh mount of Inbox must respect it.
  await page.getByRole('link', { name: 'Sessions' }).click()
  await expect(page).toHaveURL(/\/sessions$/)
  await page.getByRole('link', { name: 'Inbox' }).first().click()
  await expect(page).toHaveURL(/\/inbox$/)
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
})

test('does not redirect /inbox to /welcome once styr.welcomed is set, even with the Claude token absent', async ({
  page,
}) => {
  await page.goto('/inbox')
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()

  await page.evaluate(() => sessionStorage.setItem('styr.welcomed', '1'))
  await resetClaudeTokenAbsentAndRefetchMe(page)

  await expect(page).toHaveURL(/\/inbox$/)
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
})
