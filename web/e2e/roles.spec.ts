// T64: viewer role and per-workspace access lists. Role switching
// (/__mock/set-role) is a mock-only control route, same as workspaces.spec.ts's
// own setRole helper, so every test here is mock-only.
import { test, expect, type Page } from '@playwright/test'
import { isReal } from './helpers/seed'

interface MockWindow {
  __queryClient: { refetchQueries: (f: { queryKey: string[] }) => Promise<unknown> }
}

async function setRole(page: Page, role: 'admin' | 'member' | 'viewer') {
  await page.evaluate(async (r) => {
    const res = await fetch('/__mock/set-role', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role: r }),
    })
    if (!res.ok) throw new Error(`set-role failed: ${res.status}`)
    const client = (window as unknown as MockWindow).__queryClient
    await client.refetchQueries({ queryKey: ['me'] })
    await client.refetchQueries({ queryKey: ['workspaces'] })
    await client.refetchQueries({ queryKey: ['approvals'] })
  }, role)
}

function rowFor(page: Page, name: string) {
  return page.locator('[data-testid^="workspace-row-"]').filter({ hasText: name })
}

test.describe('Roles (viewer)', () => {
  test('a viewer has no New session button on Sessions', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'role switching is a mock-only control route')
    await page.goto('/sessions')
    await expect(page.getByRole('heading', { name: 'Sessions' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'New session' }).first()).toBeVisible()

    await setRole(page, 'viewer')
    await expect(page.getByRole('button', { name: 'New session' })).toHaveCount(0)
    // The rail's (desktop) or tab bar's (phone) read-only chip confirms the
    // role actually switched, not just that the button query happened to
    // match nothing.
    await expect(page.getByTestId('rail-readonly-chip').or(page.getByTestId('tabbar-readonly-chip'))).toBeVisible()
  })

  test('a viewer sees no Allow button on inbox cards', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'role switching is a mock-only control route')
    await page.goto('/inbox')
    await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
    const cards = page.getByTestId('needs-you-list').getByRole('listitem')
    await expect(cards).toHaveCount(2)
    await expect(page.getByRole('button', { name: 'Allow' }).first()).toBeVisible()

    await setRole(page, 'viewer')
    await expect(cards).toHaveCount(2)
    await expect(page.getByRole('button', { name: 'Allow' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Deny' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Edit and allow' })).toHaveCount(0)
    // A viewer can still navigate into the session read-only.
    await expect(page.getByRole('link', { name: 'Open session' }).first()).toBeVisible()
  })

  test('a viewer sees disabled switches on Workspaces', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'role switching is a mock-only control route')
    await page.goto('/workspaces')
    await expect(page.getByRole('heading', { name: 'Workspaces' })).toBeVisible()
    const row = rowFor(page, 'notes')
    await expect(row.getByRole('switch')).toBeEnabled()

    await setRole(page, 'viewer')
    await expect(row.getByRole('switch')).toBeDisabled()
    await expect(row.getByRole('button', { name: /^Delete/ })).toHaveCount(0)
  })
})

test.describe('Roles (workspace access)', () => {
  test('admin restricts a shared workspace to one user; an excluded member no longer sees it in the new-session dialog', async ({
    page,
  }, testInfo) => {
    test.skip(isReal(testInfo), 'role switching and the seeded second user are mock-only')

    await page.goto('/workspaces')
    await page.getByRole('button', { name: 'Add workspace' }).first().click()
    const addDialog = page.getByRole('dialog')
    await addDialog.getByRole('tab', { name: 'Server path' }).click()
    await addDialog.getByLabel('Name').fill('restricted-shared')
    await addDialog.getByLabel('Path', { exact: true }).fill('/srv/repos/restricted-shared')
    await addDialog.getByRole('button', { name: 'Add workspace' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    const row = rowFor(page, 'restricted-shared')
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: 'Everyone' }).click()

    const accessDialog = page.getByRole('dialog')
    await expect(accessDialog.getByRole('heading', { name: 'Access to restricted-shared' })).toBeVisible()
    await accessDialog.getByRole('combobox', { name: 'Access' }).click()
    await page.getByRole('option', { name: 'Listed users' }).click()
    // Sam Rivera ('u2') is the mock's second seeded user - deliberately not
    // the signed-in dev account, so switching to "member" below tests an
    // account that was never on the list.
    await accessDialog.getByRole('listitem').filter({ hasText: 'Sam Rivera' }).getByRole('checkbox').check()
    await accessDialog.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()
    await expect(row.getByRole('button', { name: 'Listed' })).toBeVisible()

    await setRole(page, 'member')
    await page.goto('/sessions')
    await page.getByRole('button', { name: 'New session' }).first().click()
    const sessionDialog = page.getByRole('dialog')
    await sessionDialog.getByRole('combobox', { name: 'Workspace' }).click()
    await expect(page.getByRole('option', { name: /restricted-shared/ })).toHaveCount(0)
    await page.keyboard.press('Escape')
  })
})
