// Fleet Gantt and the cost dashboard (T49). Both read from the read-only
// /stats routes, which the mock serves from its seeded sessions and 30 days
// of synthesised cost rows; the real routes land in T50, so the real
// projects skip.
import { test, expect } from '@playwright/test'
import { isReal } from './helpers/seed'

test.describe('Fleet Gantt', () => {
  test('renders one lane per session, with a hatched waiting segment', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T50')
    await page.goto('/sessions')
    await expect(page.getByRole('heading', { name: 'Sessions' })).toBeVisible()

    await page.getByRole('tab', { name: 'Gantt' }).click()
    await expect(page).toHaveURL(/view=gantt/)

    const gantt = page.getByTestId('fleet-gantt')
    await expect(gantt).toBeVisible()
    const lanes = page.getByTestId('gantt-lane')
    expect(await lanes.count()).toBeGreaterThanOrEqual(2)

    // Waiting is the one segment kind that does not rely on colour alone:
    // it is filled with an SVG hatch pattern.
    const waiting = page.getByTestId('gantt-segment').and(page.locator('[data-kind="waiting"]')).first()
    await expect(waiting).toBeVisible()
    expect(await waiting.getAttribute('fill')).toContain('url(#')

    // Hovering a segment names the kind and how long it lasted.
    await waiting.hover()
    await expect(page.getByTestId('gantt-tooltip')).toContainText('Waiting')

    // The window control re-asks for a different span.
    await page.getByRole('tab', { name: '24 h' }).click()
    await expect(page).toHaveURL(/window=24h/)
    await expect(page.getByTestId('fleet-gantt')).toBeVisible()

    const overflowing = await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
    )
    expect(overflowing).toBeFalsy()
  })

  test('clicking a lane opens that session', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T50')
    await page.goto('/sessions?view=gantt')
    await expect(page.getByTestId('fleet-gantt')).toBeVisible()

    await page.getByTestId('gantt-lane-link').first().click()
    await expect(page).toHaveURL(/\/sessions\/.+/)
  })
})

test.describe('Costs', () => {
  test('the cost page shows the total, 30 bars and the breakdowns', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T50')
    await page.goto('/runs/costs')
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()

    // Three tiles: today, 7 days, 30 days. The 30-day tile is the window
    // total the API reports.
    await expect(page.getByTestId('cost-tile')).toHaveCount(3)
    await expect(page.getByTestId('cost-total')).toContainText('$')

    await expect(page.getByTestId('cost-bar')).toHaveCount(30)

    // The chart is a figure with a visually hidden table behind it, so the
    // numbers are readable without the picture.
    const table = page.getByTestId('cost-table')
    await expect(table).toBeAttached()
    await expect(table.getByRole('row')).toHaveCount(31)

    await expect(page.getByRole('heading', { name: 'By user' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'By origin' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Top templates' })).toBeVisible()
    await expect(page.getByText('API-equivalent cost as reported by Claude Code')).toBeVisible()
  })

  test('phone layout: the cost page stays usable at 390px', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T50')
    await page.goto('/runs/costs')
    await expect(page.getByTestId('cost-total')).toBeVisible()
    await expect(page.getByTestId('cost-bar').first()).toBeVisible()

    const overflowing = await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
    )
    expect(overflowing).toBeFalsy()
  })
})
