// Fleet Gantt and the cost dashboard. Both read from the read-only /stats
// routes, which the mock serves from its seeded sessions and 30 days of
// synthesised cost rows - the shapes the describes above the fold assert
// against. The real lane at the bottom of this file drives
// internal/stats over a backend that has only what this run put in it.
import { test, expect } from '@playwright/test'
import { ensureRealSuccessRun, isReal, seedToolSession } from './helpers/seed'

test.describe('Fleet Gantt', () => {
  test('renders one lane per session, with a hatched waiting segment', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture sessions and cost rows; the real lane is at the bottom of this file')
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
    test.skip(isReal(testInfo), 'the mock fixture sessions and cost rows; the real lane is at the bottom of this file')
    await page.goto('/sessions?view=gantt')
    await expect(page.getByTestId('fleet-gantt')).toBeVisible()

    await page.getByTestId('gantt-lane-link').first().click()
    await expect(page).toHaveURL(/\/sessions\/.+/)
  })
})

test.describe('Costs', () => {
  test('the cost page shows the total, 30 bars and the breakdowns', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture sessions and cost rows; the real lane is at the bottom of this file')
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
    test.skip(isReal(testInfo), 'the mock fixture sessions and cost rows; the real lane is at the bottom of this file')
    await page.goto('/runs/costs')
    await expect(page.getByTestId('cost-total')).toBeVisible()
    await expect(page.getByTestId('cost-bar').first()).toBeVisible()

    const overflowing = await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
    )
    expect(overflowing).toBeFalsy()
  })
})

// --- the real backend ------------------------------------------------------

test.describe('Fleet stats against the real backend', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(!isReal(testInfo), 'drives the real backend; the mock lane is above')
  })

  test('the Gantt draws a lane for a session started in this test', async ({ page }, testInfo) => {
    const { sessionId } = await seedToolSession(page, testInfo)

    // A one-hour window: the session was created seconds ago, and internal/
    // stats derives its lane from the events that turn wrote.
    await page.goto('/sessions?view=gantt&window=1h')
    await expect(page.getByRole('tab', { name: '1 h' })).toHaveAttribute('aria-selected', 'true')

    const gantt = page.getByTestId('fleet-gantt')
    await expect(gantt).toBeVisible()
    await expect(page.locator(`[data-testid="gantt-lane-link"][href$="/sessions/${sessionId}"]`)).toBeVisible()

    const lane = page.getByTestId('gantt-lane').filter({ has: page.locator(`[href$="/sessions/${sessionId}"]`) })
    await expect(lane.getByTestId('gantt-segment').first()).toBeVisible()

    const overflowing = await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
    )
    expect(overflowing).toBeFalsy()
  })

  test('the cost page totals what this backend has actually spent', async ({ page }) => {
    // A finished run, so there is a session with a cost on it: the fake
    // replays a recorded result whose total_cost_usd is non-zero.
    await ensureRealSuccessRun(page)

    await page.goto('/runs/costs')
    const total = page.getByTestId('cost-total')
    await expect(total).toContainText('$')
    const usd = Number((await total.innerText()).replace(/[^0-9.]/g, ''))
    expect(usd).toBeGreaterThanOrEqual(0)

    // GET /stats/costs?days=30 always answers with every day in the window,
    // spent or not, so the chart has 30 bars on a one-day-old database.
    await expect(page.getByTestId('cost-bar')).toHaveCount(30)
    await expect(page.getByTestId('cost-table').getByRole('row')).toHaveCount(31)

    // Sessions this suite started carry origin `ui`; a schedule's carry
    // `schedule`. Either is proof the breakdown is reading real rows.
    const byOrigin = page.getByRole('heading', { name: 'By origin' }).locator('..')
    await expect(byOrigin.getByRole('cell', { name: /^(ui|schedule)$/ }).first()).toBeVisible()
  })
})
