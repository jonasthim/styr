import { test, expect } from '@playwright/test'
import { isReal, setClaudeTokenPresence } from './helpers/seed'

// Runs against both backends. /api/v1/me starts logged in as the dev admin
// with a present Claude token in both, so /inbox loads normally by
// default; these tests flip the token to absent in place via
// setClaudeTokenPresence (e2e/helpers/seed.ts) - the mock's POST
// /__mock/reset-claude-token control route, or DELETE
// /api/v1/me/claude-token on the real backend - the same pattern
// e2e/profile.spec.ts uses, since a fresh page.goto() would re-evaluate
// the mock handlers module and reset that flag right back. Real mode
// restores the token afterwards (setClaudeTokenPresence(..., true)): the
// real projects share one backend and account across every spec file
// (workers: 1, web/playwright.config.ts), and later files create sessions
// that require one.

test.describe.configure({ mode: 'serial' })

test('redirects /inbox to /welcome when the Claude token is absent and onboarding has not been seen, and Skip does not bounce back', async ({
  page,
}, testInfo) => {
  await page.goto('/inbox')
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()

  // Starting state for a fresh browser context: no styr.welcomed flag yet.
  await page.evaluate(() => sessionStorage.removeItem('styr.welcomed'))
  await setClaudeTokenPresence(page, testInfo, false)

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

  if (isReal(testInfo)) await setClaudeTokenPresence(page, testInfo, true)
})

test('does not redirect /inbox to /welcome once styr.welcomed is set, even with the Claude token absent', async ({
  page,
}, testInfo) => {
  await page.goto('/inbox')
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()

  await page.evaluate(() => sessionStorage.setItem('styr.welcomed', '1'))
  await setClaudeTokenPresence(page, testInfo, false)

  await expect(page).toHaveURL(/\/inbox$/)
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()

  if (isReal(testInfo)) await setClaudeTokenPresence(page, testInfo, true)
})
