import { test, expect, type Page } from '@playwright/test'

// Runs against the mock backend (see e2e/login.spec.ts for the general mock
// setup notes). /api/v1/me starts logged in as the dev admin with a present
// Claude token; the absent-state tests flip it via POST
// /__mock/reset-claude-token (src/mocks/handlers.ts) instead of relying on a
// fresh page load, matching the pattern e2e/login.spec.ts uses for the
// logged-out flag.

interface MockWindow {
  __queryClient: { refetchQueries: (f: { queryKey: string[] }) => Promise<unknown> }
}

// A second page.goto() would reload the document and re-evaluate the mock
// handlers module, undoing the reset (see e2e/login.spec.ts's note on the
// same pattern for the logged-out flag) - so reset and refetch in place on
// a page /profile is already loaded on. refetchQueries (not
// invalidateQueries) is awaited so the DOM has already re-rendered with the
// absent state by the time this call returns.
async function resetClaudeTokenAbsent(page: Page) {
  await page.evaluate(async () => {
    const res = await fetch('/__mock/reset-claude-token', { method: 'POST' })
    if (!res.ok) throw new Error(`reset-claude-token failed: ${res.status}`)
    await (window as unknown as MockWindow).__queryClient.refetchQueries({ queryKey: ['me'] })
  })
}

test.describe('Profile - Claude token card', () => {
  test('shows the absent state and an entered token flips it to present', async ({ page }) => {
    await page.goto('/profile')
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()
    await resetClaudeTokenAbsent(page)

    const card = page.getByTestId('claude-token-card')
    await expect(card.getByTestId('claude-token-card-absent')).toBeVisible()
    await expect(card.getByRole('button', { name: 'Save and verify' })).toBeVisible()

    const input = card.getByLabel('Claude token')
    await input.fill('sk-ant-oat01-testtoken')
    await card.getByRole('button', { name: 'Save and verify' }).click()

    const present = card.getByTestId('claude-token-card-present')
    await expect(present).toBeVisible()
    const label = present.locator('p').first()
    await expect(label).toHaveText(/token$/)
    await expect(present.getByText(/Verified/)).toBeVisible()
    await expect(card.getByRole('button', { name: 'Replace' })).toBeVisible()
    await expect(card.getByRole('button', { name: 'Remove' })).toBeVisible()
  })

  test('an invalid token shows the 422 message', async ({ page }) => {
    await page.goto('/profile')
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()
    await resetClaudeTokenAbsent(page)

    const card = page.getByTestId('claude-token-card')
    const input = card.getByLabel('Claude token')
    await input.fill('bad')
    await card.getByRole('button', { name: 'Save and verify' }).click()

    const error = card.getByTestId('claude-token-card-error')
    await expect(error).toBeVisible()
    await expect(error).toHaveText(/wasn't accepted/)
    // Still absent: a failed verify must not have stored anything.
    await expect(card.getByTestId('claude-token-card-absent')).toBeVisible()
  })
})

test('/settings lists three builtin profiles and a five-option mode select', async ({ page }) => {
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible()

  await expect(page.getByTestId('profile-row-interactive')).toBeVisible()
  await expect(page.getByTestId('profile-row-investigate')).toBeVisible()
  await expect(page.getByTestId('profile-row-remediate')).toBeVisible()
  await expect(page.locator('[data-testid^="profile-row-"]').filter({ hasText: 'builtin' })).toHaveCount(3)

  // A builtin row's mode is locked, so the five allowed modes are read off
  // the one editable profile's select. The mode select is a Radix Select
  // (role=combobox opening a role=listbox of role=option items), not a
  // native <select>, so the options only exist in the DOM while it is open.
  const modeSelect = page.getByTestId('profile-row-custom').getByRole('combobox')
  await expect(modeSelect).toHaveText('Default')
  await modeSelect.click()

  const options = page.getByRole('option')
  await expect(options).toHaveCount(5)
  expect((await options.allInnerTexts()).sort()).toEqual(
    ['Accept edits', 'Auto', 'Default', "Don't ask", 'Plan'].sort(),
  )

  // A builtin's mode select is present and shows its value, but cannot be changed.
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('profile-row-interactive').getByRole('combobox')).toBeDisabled()
})

test('/workspaces lists two workspaces', async ({ page }) => {
  await page.goto('/workspaces')
  await expect(page.getByRole('heading', { name: 'Workspaces' })).toBeVisible()
  await expect(page.locator('[data-testid^="workspace-row-"]')).toHaveCount(2)
  await expect(page.getByTestId('workspace-row-w1')).toContainText('styr')
  await expect(page.getByTestId('workspace-row-w2')).toContainText('notes')
})

test('/welcome shows three steps and Skip lands on /inbox', async ({ page }) => {
  await page.goto('/welcome')
  await expect(page.getByRole('heading', { name: 'Welcome to Styr' })).toBeVisible()
  await expect(page.getByTestId('onboarding-step-1')).toBeVisible()
  await expect(page.getByTestId('onboarding-step-2')).toBeVisible()
  await expect(page.getByTestId('onboarding-step-3')).toBeVisible()

  // Keep the mouse away from the rail on the way to Skip: a click's path
  // crossing the left edge can trigger its hover-expand, which then
  // intercepts the pointer (see e2e/shell.spec.ts's "[ pins the rail open"
  // test for the same guard).
  await page.mouse.move(700, 400)
  await page.getByRole('button', { name: 'Skip' }).click()
  await expect(page).toHaveURL(/\/inbox$/)
})
