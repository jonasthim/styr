import path from 'node:path'
import { test, expect, type Page } from '@playwright/test'
import { ensureRealToken, isReal, setClaudeTokenPresence } from './helpers/seed'

// Runs against both backends. /api/v1/me starts logged in as the dev admin
// with a present Claude token in both; the absent-state tests flip it via
// setClaudeTokenPresence (e2e/helpers/seed.ts) instead of relying on a
// fresh page load, matching the pattern e2e/login.spec.ts uses for the
// mock's logged-out flag. The dev-mode token verifier (both backends:
// web/src/mocks/handlers.ts's verifyToken and cmd/styr/wire.go's
// devVerifier) accepts anything starting with "sk-ant-" and rejects
// everything else, so the same "…testtoken" / "bad" inputs exercise the
// same accept/reject paths in both modes.

test.describe.configure({ mode: 'serial' })

test.describe('Profile - Claude token card', () => {
  test('shows the absent state and an entered token flips it to present', async ({ page }, testInfo) => {
    await page.goto('/profile')
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()
    await setClaudeTokenPresence(page, testInfo, false)

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
    // Ends present (with this test's own token) either way - nothing to
    // restore for later real-mode tests.
  })

  test('an invalid token shows the 422 message', async ({ page }, testInfo) => {
    await page.goto('/profile')
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()
    await setClaudeTokenPresence(page, testInfo, false)

    const card = page.getByTestId('claude-token-card')
    const input = card.getByLabel('Claude token')
    await input.fill('bad')
    await card.getByRole('button', { name: 'Save and verify' }).click()

    const error = card.getByTestId('claude-token-card-error')
    await expect(error).toBeVisible()
    // ClaudeTokenCard shows the server's own error message verbatim
    // (ApiError.message), and the two dev-mode verifiers word it
    // differently: the mock's is longer UX copy (web/src/mocks/handlers.ts's
    // TOKEN_INVALID_MESSAGE), the real handler's a short one
    // (internal/api/me_handlers.go's handleMeTokenPut) - both are the same
    // 422 token_invalid outcome for the same "bad" input.
    await expect(error).toHaveText(isReal(testInfo) ? /could not be verified/ : /wasn't accepted/)
    // Still absent: a failed verify must not have stored anything.
    await expect(card.getByTestId('claude-token-card-absent')).toBeVisible()

    // Restore before this file's other tests (and every later real spec
    // file, given workers: 1 - web/playwright.config.ts) need a working
    // Claude token to create sessions.
    if (isReal(testInfo)) await ensureRealToken(page)
  })
})

/** Id and name of a profile whose mode select is editable, creating one if
 * the backend under test has none. Both are needed: the row is addressed by
 * id (its data-testid) and the selects inside it by the profile's name (their
 * accessible label), which are only the same string by coincidence in the
 * mock's seed data. Builtin rows lock their mode (docs/openapi.yaml), and a
 * disabled Radix Select cannot be opened - so the allowed modes can only be
 * read off an editable row.
 *
 * Fetches from inside the page rather than through page.request: in mock mode
 * the API only exists in the service worker (web/src/mocks/), which
 * Playwright's own request context would bypass. Returns `created` so the
 * caller can reload once when a profile had to be added - only ever the case
 * against the real backend, whose state survives a reload (the mock seeds an
 * editable profile, and reloading it would re-seed everything else too). */
async function editableProfile(page: Page): Promise<{ id: string; name: string; created: boolean }> {
  return page.evaluate(async () => {
    const headers = { 'Content-Type': 'application/json', 'X-Requested-With': 'styr' }
    const list = await fetch('/api/v1/profiles', { headers })
    if (!list.ok) throw new Error(`list profiles failed: ${list.status}`)
    const existing = ((await list.json()) as Array<{ id: string; name: string; builtin: boolean }>).find((p) => !p.builtin)
    if (existing) return { id: existing.id, name: existing.name, created: false }

    const created = await fetch('/api/v1/profiles', {
      method: 'POST',
      headers,
      body: JSON.stringify({ name: 'styr-e2e-modes', mode: 'default' }),
    })
    if (!created.ok) throw new Error(`create profile failed: ${created.status}`)
    const profile = (await created.json()) as { id: string; name: string }
    return { id: profile.id, name: profile.name, created: true }
  })
}

test('/settings lists three builtin profiles and a five-option mode select', async ({ page }) => {
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible()
  const { id: editableId, name: editableName, created } = await editableProfile(page)
  if (created) {
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible()
  }

  await expect(page.getByTestId('profile-row-interactive')).toBeVisible()
  await expect(page.getByTestId('profile-row-investigate')).toBeVisible()
  await expect(page.getByTestId('profile-row-remediate')).toBeVisible()
  await expect(page.locator('[data-testid^="profile-row-"]').filter({ hasText: 'builtin' })).toHaveCount(3)

  // The mode select is a Radix Select now (role=combobox opening role=option
  // items in a portal), not a native <select>, so its options only exist in
  // the DOM while it is open - they can no longer be read off a closed,
  // disabled builtin row the way <option> elements could. A row carries three
  // comboboxes since card T38 (mode, model, effort), so each is addressed by
  // its accessible name.
  const modeSelect = page
    .getByTestId(`profile-row-${editableId}`)
    .getByRole('combobox', { name: `Mode for ${editableName}` })
  await expect(modeSelect).toHaveText('Default')
  await modeSelect.click()

  const options = page.getByRole('option')
  await expect(options).toHaveCount(5)
  expect((await options.allInnerTexts()).sort()).toEqual(['Accept edits', 'Auto', 'Default', "Don't ask", 'Plan'].sort())

  // A builtin's mode still shows its value, but cannot be changed.
  await page.keyboard.press('Escape')
  const builtinSelect = page
    .getByTestId('profile-row-interactive')
    .getByRole('combobox', { name: 'Mode for interactive' })
  await expect(builtinSelect).toHaveText('Default')
  await expect(builtinSelect).toBeDisabled()
})

test('/workspaces lists at least the workspaces this run created', async ({ page }, testInfo) => {
  if (isReal(testInfo)) {
    // A fresh real backend starts with none, and by the time this test runs
    // other real-mode spec files (workers: 1 serializes the whole run, see
    // web/playwright.config.ts) may already have created the shared
    // "styr-e2e" workspace (e2e/helpers/seed.ts's ensureRealWorkspace) -
    // an exact count is therefore fragile; this checks two distinct
    // workspaces created here are present and correctly rendered instead.
    // Both point at the repo root (path.resolve, like e2e/helpers/seed.ts's
    // ensureRealWorkspace) - only name is unique in the workspaces table
    // (internal/db/migrations/00001_init.sql), so two rows may share a path.
    const repoRoot = path.resolve(process.cwd(), '..')
    // Unique per project: this file's workspace names must not collide with
    // real-desktop's and real-phone's own runs of this same test against
    // the one shared backend (workers: 1, web/playwright.config.ts) - name
    // is the only unique column on workspaces (00001_init.sql).
    const names = [`styr-e2e-profile-${testInfo.project.name}-0`, `styr-e2e-profile-${testInfo.project.name}-1`]
    for (const name of names) {
      const res = await page.request.post('/api/v1/workspaces', {
        headers: { 'X-Requested-With': 'styr' },
        data: { name, source: 'path', path: repoRoot, default_profile_id: 'interactive' },
      })
      if (!res.ok()) throw new Error(`seed: create workspace ${name} failed: ${res.status()}`)
    }
    await page.goto('/workspaces')
    await expect(page.getByRole('heading', { name: 'Workspaces' })).toBeVisible()
    // getByText below auto-retries until the list finishes loading; a bare
    // rows.count() would not (it reads the DOM once, racing the initial
    // fetch), so the count check runs after those two settle instead of
    // before them.
    await expect(page.getByText(names[0])).toBeVisible()
    await expect(page.getByText(names[1])).toBeVisible()
    const rows = page.locator('[data-testid^="workspace-row-"]')
    expect(await rows.count()).toBeGreaterThanOrEqual(2)
  } else {
    await page.goto('/workspaces')
    await expect(page.getByRole('heading', { name: 'Workspaces' })).toBeVisible()
    await expect(page.locator('[data-testid^="workspace-row-"]')).toHaveCount(2)
    await expect(page.getByTestId('workspace-row-w1')).toContainText('styr')
    await expect(page.getByTestId('workspace-row-w2')).toContainText('notes')
  }
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
