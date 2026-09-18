// Runs (T33): the list, filtering by outcome, a run's report rendered from
// its schema, re-running it, and the phone layout. Mock only for now (real
// backend for this contract arrives in T34/T35); see triggers.spec.ts for
// triggers, templates and notifications.
import { test, expect } from '@playwright/test'
import { isReal } from './helpers/seed'

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
