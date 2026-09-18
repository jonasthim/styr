// Runs (T33): the list, filtering by outcome, a run's report rendered from
// its schema, re-running it, and the phone layout. The mock's seeded runs
// cover outcomes the real pipeline cannot be pushed into quickly (a run
// stuck at needs_human, a re-run chain); the "Runs (real backend)" block at
// the bottom filters the list against a real run the trigger pipeline
// actually produced (T37). See triggers.spec.ts for the full real pipeline.
import { test, expect } from '@playwright/test'
import { ensureRealSuccessRun, isReal } from './helpers/seed'

// Fixed mock ids (web/src/mocks/triggersHandlers.ts) - duplicated as
// literals; e2e specs run outside Vite (see triggers.spec.ts's own comment).
const RUN_SUCCESS_ID = 'run-success-1'

test.describe('Runs', () => {
  test('filters the list by outcome', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto('/runs')
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()
    await expect(page.getByTestId('run-row').first()).toBeVisible()

    const initialCount = await page.getByTestId('run-row').count()
    expect(initialCount).toBeGreaterThanOrEqual(3)

    await page.getByRole('button', { name: 'Needs you' }).click()
    await expect(page).toHaveURL(/outcome=needs_human/)
    await expect(page.getByTestId('run-row')).toHaveCount(1)
    await expect(page.getByTestId('run-row')).toContainText('Investigation needs a decision')

    await page.getByRole('button', { name: 'All' }).click()
    await expect(page).not.toHaveURL(/outcome=/)
    await expect(page.getByTestId('run-row')).toHaveCount(initialCount)
  })

  test('run detail renders severity, diagnosis and evidence from the report schema', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto(`/runs/${RUN_SUCCESS_ID}`)

    const reportCard = page.getByTestId('run-report')
    await expect(reportCard.getByText('critical', { exact: true })).toBeVisible()
    await expect(reportCard.getByText(/prod-01 memory usage climbed/)).toBeVisible()
    await expect(reportCard.getByText(/journalctl -u styr-api/)).toBeVisible()
    await expect(reportCard.getByText(/Restart styr-api/)).toBeVisible()
    await expect(reportCard.getByText('82%')).toBeVisible()

    await expect(page.getByText('Delivery payload')).toBeVisible()

    await page.getByRole('link', { name: 'Open session' }).click()
    await expect(page).toHaveURL(/\/sessions\//)
    await expect(page.getByText('Unattended run')).toBeVisible()
  })

  test('re-run starts a fresh run and navigates to it', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto(`/runs/${RUN_SUCCESS_ID}`)

    await page.getByRole('button', { name: 'Re-run' }).click()
    await expect(page.getByTestId('toast').filter({ hasText: 'Re-run started' })).toBeVisible()
    await expect(page).toHaveURL(/\/runs\/(?!run-success-1$).+/)
    await expect(page.getByText('Running', { exact: true }).first()).toBeVisible()
  })

  test('phone layout: the runs list stays usable at 390px', async ({ page }, testInfo) => {
    const isPhone = testInfo.project.name.includes('phone')
    // The mock seeds runs; the real backend only has the ones this suite's
    // own deliveries produced, and this test needs a row to measure.
    if (isReal(testInfo)) await ensureRealSuccessRun(page)
    await page.goto('/runs')
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()

    if (isPhone) {
      await expect(page.getByTestId('tabbar')).toBeVisible()
      await expect(page.getByTestId('rail')).toHaveCount(0)
    } else {
      await expect(page.getByTestId('rail')).toBeVisible()
    }

    const row = page.getByTestId('run-row').first()
    await expect(row).toBeVisible()
    const overflowing = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1)
    expect(overflowing).toBeFalsy()
  })
})

// --- real backend ----------------------------------------------------------
test.describe('Runs (real backend)', () => {
  test('filter chips narrow the list to a real successful run', async ({ page }, testInfo) => {
    test.skip(!isReal(testInfo), 'real-backend only: needs a run the trigger pipeline produced')
    const runId = await ensureRealSuccessRun(page)

    await page.goto('/runs')
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()
    await expect(page.getByTestId('run-row').first()).toBeVisible()

    await page.getByRole('button', { name: 'Success' }).click()
    await expect(page).toHaveURL(/outcome=success/)
    const successRows = page.getByTestId('run-row')
    await expect(successRows.filter({ hasText: /Disk on host x at 91%/ }).first()).toBeVisible()
    // Every row the success filter left is a success run.
    for (const label of await successRows.evaluateAll((rows) => rows.map((r) => r.getAttribute('aria-label') ?? ''))) {
      expect(label.startsWith('Success')).toBeTruthy()
    }

    // An outcome nothing in this run's data has empties the list.
    await page.getByRole('button', { name: 'Timed out' }).click()
    await expect(page).toHaveURL(/outcome=timeout/)
    await expect(page.getByTestId('run-row')).toHaveCount(0)

    await page.getByRole('button', { name: 'All' }).click()
    await expect(page).not.toHaveURL(/outcome=/)
    await expect(page.getByTestId('run-row').first()).toBeVisible()

    // The filtered row still leads to its own run.
    await page.goto(`/runs/${runId}`)
    await expect(page.getByTestId('run-report')).toBeVisible()
  })
})
