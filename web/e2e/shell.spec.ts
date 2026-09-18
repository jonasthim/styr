import { test, expect, type Page } from '@playwright/test'

// Runs against the mock backend (see e2e/login.spec.ts for the general mock
// setup notes). /api/v1/me starts logged in as the dev admin, so every test
// here can navigate straight to an authenticated route. The mock seeds two
// pending approvals, both against the same session.

// Waits for the placeholder page's heading (proof the route rendered inside
// the Shell) plus a beat for Shell's passive effects - useShortcuts' global
// keydown listener attaches a moment after paint, and firing a keyboard
// shortcut in that gap is a real (if rare) source of flake.
async function gotoReady(page: Page, path: string, heading: string) {
  await page.goto(path)
  await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  await page.waitForTimeout(50)
}

test('Cmd/Ctrl+K opens the palette; typing "sess" then Enter lands on /sessions', async ({ page }) => {
  await gotoReady(page, '/inbox', 'Inbox')

  await page.keyboard.press('Control+k')
  const input = page.getByRole('combobox')
  await expect(input).toBeVisible()

  await input.fill('sess')
  await page.keyboard.press('Enter')

  await expect(page).toHaveURL(/\/sessions$/)
  await expect(page.getByRole('heading', { name: 'Sessions' })).toBeVisible()
  await expect(input).toBeHidden()
})

test('Esc closes the palette without navigating', async ({ page }) => {
  await gotoReady(page, '/inbox', 'Inbox')

  await page.keyboard.press('Control+k')
  await expect(page.getByRole('combobox')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('combobox')).toBeHidden()
  await expect(page).toHaveURL(/\/inbox$/)
})

test('pressing g then i navigates to /inbox', async ({ page }) => {
  await gotoReady(page, '/sessions', 'Sessions')

  await page.keyboard.press('g')
  await page.keyboard.press('i')

  await expect(page).toHaveURL(/\/inbox$/)
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
})

test('t flips data-theme on <html>', async ({ page }) => {
  await gotoReady(page, '/inbox', 'Inbox')
  const html = page.locator('html')
  await expect(html).toHaveAttribute('data-theme', 'dark')

  await page.keyboard.press('t')
  await expect(html).toHaveAttribute('data-theme', 'light')

  await page.keyboard.press('t')
  await expect(html).toHaveAttribute('data-theme', 'dark')
})

test('the Inbox badge shows the pending approvals count', async ({ page }) => {
  await page.goto('/inbox')
  await expect(page.getByTestId('inbox-badge')).toHaveText('2')
})

test('the rail and tab bar follow the viewport breakpoint', async ({ page }, testInfo) => {
  await page.goto('/inbox')
  const isPhone = testInfo.project.name.includes('phone')

  if (isPhone) {
    await expect(page.getByTestId('tabbar')).toBeVisible()
    await expect(page.getByTestId('rail')).toHaveCount(0)
  } else {
    await expect(page.getByTestId('rail')).toBeVisible()
    await expect(page.getByTestId('tabbar')).toHaveCount(0)
  }
})

test('n navigates to /sessions?new=1', async ({ page }) => {
  await gotoReady(page, '/inbox', 'Inbox')
  await page.keyboard.press('n')
  await expect(page).toHaveURL(/\/sessions\?new=1$/)
})

test('[ pins the rail open on desktop', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name.includes('phone'), 'rail only renders on desktop')
  await gotoReady(page, '/inbox', 'Inbox')
  // Keep the mouse away from the rail so hover-expand can't fake a pass.
  await page.mouse.move(700, 400)
  const rail = page.getByTestId('rail')
  await expect(rail).toHaveCSS('width', '56px')
  await page.keyboard.press('[')
  await expect(rail).toHaveCSS('width', '220px')
})

test('? opens the shortcuts cheat sheet', async ({ page }) => {
  await gotoReady(page, '/inbox', 'Inbox')
  await page.keyboard.press('?')
  await expect(page.getByTestId('shortcuts-dialog')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Shortcuts' })).toBeVisible()
})
