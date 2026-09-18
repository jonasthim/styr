import { test, expect } from '@playwright/test'
import { isReal } from './helpers/seed'

// Runs against both backends. The mock (msw handlers + FakeEventSource; see
// web/src/mocks/) seeds a providers list with exactly one provider
// ("Authentik") and a "logged out" flag the two mock-only tests below flip;
// the real backend's dev mode has no OIDC providers configured and always
// auto-logs the dev user in (cmd/styr/wire.go's devUser bypass), so those
// two scenarios have nothing to exercise there.

test('login page shows the wordmark and one provider button', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: dev mode auto-login has no providers to list')
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Styr' })).toBeVisible()
  await expect(page.getByRole('link', { name: /^Continue with/ })).toHaveCount(1)
  await expect(page.getByRole('link', { name: 'Continue with Authentik' })).toBeVisible()
})

test('/ redirects to /inbox when /api/v1/me returns a user', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/inbox$/)
  // AuthGate renders nothing until `me` resolves, so waiting for the
  // placeholder's own heading (rather than just the URL) rules out a
  // false pass from a blank page that merely sits at the right path.
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
})

test('/ lands on /login when the session is logged out', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: dev mode auto-login bypasses the login session entirely')
  // A hard page reload would re-evaluate the mock handlers module and reset
  // its "logged out" flag along with the rest of the page's JS, so this
  // flips the flag and forces a refetch within the same page session
  // instead of navigating again. The msw worker only intercepts requests
  // the page itself makes (not Playwright's Node-side request context), so
  // the toggle runs via page.evaluate too.
  await page.goto('/inbox')
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
  await page.evaluate(async () => {
    await fetch('/__mock/logout', { method: 'POST' })
  })
  await page.evaluate(() => {
    const client = (window as unknown as { __queryClient: { invalidateQueries: (f: { queryKey: string[] }) => void } })
      .__queryClient
    client.invalidateQueries({ queryKey: ['me'] })
  })
  await expect(page).toHaveURL(/\/login$/)
})
