// Schedules and loops: the cron table and its create dialog with the live
// next-run preview, the enabled switch, "Run now", the firings drawer
// (including a skipped_overlap the real scheduler only produces when a run
// overruns its own cadence), and the Loops tab under Runs with its Stop
// confirmation.
//
// The describes above the fold read the mock's seeded schedules, loop and
// template by name, so they are the mock lane; the real backend drives the
// same surfaces from scratch at the bottom of this file.
import { test, expect } from '@playwright/test'
import {
  ensureRealLoopTemplate,
  ensureRealScheduleTemplate,
  isReal,
  LOOP_TEMPLATE_NAME as REAL_LOOP_TEMPLATE_NAME,
  SCHEDULE_TEMPLATE_NAME,
} from './helpers/seed'

// Fixed mock ids and names (web/src/mocks/schedulesHandlers.ts) - duplicated
// as literals rather than imported: e2e specs run outside Vite, so they
// can't pull in a mock module (see triggers.spec.ts's own note).
const SCHEDULE_ENABLED_NAME = 'Nightly dependency sweep'
const SCHEDULE_DISABLED_NAME = 'Weekly changelog digest'
const LOOP_TEMPLATE_NAME = 'Fix failing CI until green'

test.describe('Schedules', () => {
  test('create a schedule and see the cron preview', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/schedules')
    await expect(page.getByRole('heading', { name: 'Schedules' })).toBeVisible()

    // first(): an account with no schedules yet also shows the empty
    // state's own "New schedule" button, and whether it has rendered by now
    // depends on whether GET /schedules has come back. Both open the same
    // dialog, so the header's is taken either way.
    await page.getByRole('button', { name: 'New schedule' }).first().click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: 'New schedule' })).toBeVisible()

    await dialog.getByLabel('Name').fill('Morning log review')
    await dialog.getByRole('combobox', { name: 'Template' }).click()
    await page.getByRole('option', { name: 'Grafana alert investigation' }).click()

    await dialog.getByLabel('Cron').fill('30 7 * * *')

    // The preview is debounced against POST /schedules/preview: a human
    // description plus the next five times it would fire.
    const preview = page.getByTestId('cron-preview')
    await expect(preview).toContainText('every day at 07:30')
    await expect(page.getByTestId('cron-preview-time')).toHaveCount(5)

    await dialog.getByRole('button', { name: 'Create schedule' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    const row = page.getByTestId(/^schedule-row-/).filter({ hasText: 'Morning log review' })
    await expect(row).toBeVisible()
    await expect(row).toContainText('30 7 * * *')
  })

  test('an invalid vars object blocks the save', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/schedules')
    await page.getByRole('button', { name: 'New schedule' }).click()
    const dialog = page.getByRole('dialog')

    await dialog.getByLabel('Name').fill('Broken vars')
    await dialog.getByLabel('Variables').fill('{not json')
    await expect(dialog.getByText('This is not valid JSON.')).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Create schedule' })).toBeDisabled()
  })

  test('toggle a schedule enabled', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/schedules')
    const toggle = page.getByRole('switch', { name: `Enabled for ${SCHEDULE_ENABLED_NAME}` })
    await expect(toggle).toHaveAttribute('aria-checked', 'true')

    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'false')

    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
  })

  test('Run now starts a run and links to it', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/schedules')

    const row = page.getByTestId(/^schedule-row-/).filter({ hasText: SCHEDULE_DISABLED_NAME })
    await row.getByRole('button', { name: 'Run now' }).click()

    await expect(page.getByTestId('toast').filter({ hasText: 'Run started' })).toBeVisible()
    await expect(page).toHaveURL(/\/runs\/.+/)
  })

  test('the firings drawer shows a skipped overlap', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/schedules')

    const row = page.getByTestId(/^schedule-row-/).filter({ hasText: SCHEDULE_ENABLED_NAME })
    await row.getByRole('button', { name: 'Firings' }).click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: `Firings — ${SCHEDULE_ENABLED_NAME}` })).toBeVisible()

    const skipped = page.getByTestId('firing-row').filter({ hasText: 'Skipped' })
    await expect(skipped.first()).toBeVisible()
    await expect(skipped.first()).toContainText('still running')
    await expect(page.getByTestId('firing-row').filter({ hasText: 'Started' }).first()).toBeVisible()
  })

  test('phone layout: the schedules table stays usable at 390px', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    const isPhone = testInfo.project.name.includes('phone')
    await page.goto('/schedules')
    await expect(page.getByRole('heading', { name: 'Schedules' })).toBeVisible()
    await expect(page.getByTestId(/^schedule-row-/).first()).toBeVisible()

    if (isPhone) {
      await expect(page.getByTestId('tabbar')).toBeVisible()
      await expect(page.getByTestId('rail')).toHaveCount(0)
    }
    const overflowing = await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
    )
    expect(overflowing).toBeFalsy()
  })
})

test.describe('Loops', () => {
  test('the loops tab lists the running loop and Stop asks first', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/runs/loops')
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()

    const row = page.getByTestId(/^loop-row-/).filter({ hasText: LOOP_TEMPLATE_NAME })
    await expect(row).toBeVisible()
    await expect(row).toContainText('2/3')

    // The drawer lists one accordion entry per iteration, with that
    // iteration's report inside.
    await row.getByRole('button', { name: 'Details' }).click()
    const drawer = page.getByRole('dialog')
    await expect(drawer.getByRole('group')).toHaveCount(2)
    await drawer.getByRole('button', { name: /Iteration 2/ }).click()
    await expect(drawer.getByText(/still failing/i).first()).toBeVisible()
    await drawer.getByRole('button', { name: 'Close' }).click()

    await row.getByRole('button', { name: 'Stop' }).click()
    const confirm = page.getByRole('dialog')
    await expect(confirm.getByRole('heading', { name: 'Stop this loop?' })).toBeVisible()
    await confirm.getByRole('button', { name: 'Stop loop' }).click()

    await expect(page.getByTestId('toast').filter({ hasText: 'Loop stopped' })).toBeVisible()
    await expect(row).toContainText('Stopped')
    await expect(row.getByRole('button', { name: 'Stop' })).toHaveCount(0)
  })

  test('a loop run links back to its loop from the run detail', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/runs')
    const row = page.getByTestId('run-row').filter({ hasText: 'still failing' }).first()
    await row.click()

    const chip = page.getByTestId('loop-chip')
    await expect(chip).toContainText('Iteration')
    await chip.click()
    await expect(page).toHaveURL(/\/runs\/loops/)
  })
})

test.describe('Template loops', () => {
  test('the template editor sets a loop and the list shows the chip', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/triggers?tab=templates')
    const row = page.getByTestId(/^template-row-/).filter({ hasText: LOOP_TEMPLATE_NAME })
    await expect(row.getByText('Loop', { exact: true })).toBeVisible()

    await page.goto('/templates/tmpl-grafana')
    await expect(page.getByRole('heading', { name: 'Grafana alert investigation' })).toBeVisible()
    await expect(page.getByText('Runs again on the same session until the report')).toBeVisible()

    await page.getByRole('combobox', { name: 'Until field' }).click()
    await page.getByRole('option', { name: 'resolved_itself' }).click()
    await page.getByLabel('Max iterations').fill('4')
    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByTestId('toast').filter({ hasText: 'Template saved' })).toBeVisible()

    // Back through the app rather than page.goto: a reload re-evaluates the
    // mock modules and takes the just-saved loop with them.
    await page.getByRole('link', { name: 'Templates' }).click()
    await expect(
      page.getByTestId(/^template-row-/).filter({ hasText: 'Grafana alert investigation' }).getByText('Loop', { exact: true }),
    ).toBeVisible()
  })

  test('Run now on a template opens the run it started', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture rows; the real lane is at the bottom of this file')
    await page.goto('/templates/tmpl-grafana')
    await page.getByRole('button', { name: 'Run now' }).click()
    await expect(page).toHaveURL(/\/runs\/.+/)
    await expect(page.getByText('Running', { exact: true }).first()).toBeVisible()
  })
})

// --- the real backend ------------------------------------------------------
// One scheduler, one run engine and one shared SQLite database (workers: 1,
// see playwright.config.ts), driving the routes internal/api/
// schedules_handlers.go and loops_handlers.go serve. Every template these
// fire renders the "[fixture:06]" marker, so the shell fake replays a
// recorded --json-schema session and the runs finish with a real structured
// report.
test.describe('Schedules against the real backend', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(!isReal(testInfo), 'drives the real backend; the mock lane is above')
  })

  test('a schedule is created, previewed, fired by hand and switched off', async ({ page }) => {
    await ensureRealScheduleTemplate(page)
    const name = `Minute sweep ${Date.now()}`

    await page.goto('/schedules')
    await expect(page.getByRole('heading', { name: 'Schedules' })).toBeVisible()

    // first(): an account with no schedules yet also shows the empty
    // state's own "New schedule" button, and whether it has rendered by now
    // depends on whether GET /schedules has come back. Both open the same
    // dialog, so the header's is taken either way.
    await page.getByRole('button', { name: 'New schedule' }).first().click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Name').fill(name)
    await dialog.getByRole('combobox', { name: 'Template' }).click()
    await page.getByRole('option', { name: SCHEDULE_TEMPLATE_NAME }).click()
    await dialog.getByLabel('Cron').fill('* * * * *')

    // The preview is the scheduler's own answer, computed by robfig/cron
    // and phrased by internal/schedules/cron.go's describeCron.
    const preview = page.getByTestId('cron-preview')
    await expect(preview).toContainText('every minute')
    await expect(page.getByTestId('cron-preview-time')).toHaveCount(5)

    await dialog.getByRole('button', { name: 'Create schedule' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    const row = page.getByTestId(/^schedule-row-/).filter({ hasText: name })
    await expect(row).toBeVisible()
    await expect(row).toContainText('* * * * *')

    // "Run now" ignores the cron and fires immediately, then follows the
    // run it started.
    await row.getByRole('button', { name: 'Run now' }).click()
    await expect(page.getByTestId('toast').filter({ hasText: 'Run started' })).toBeVisible()
    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}$/)
    await expect(page.getByText('Running', { exact: true }).first()).toBeVisible()

    // Back on the table, that firing is in the schedule's history.
    await page.goto('/schedules')
    const sameRow = page.getByTestId(/^schedule-row-/).filter({ hasText: name })
    await sameRow.getByRole('button', { name: 'Firings' }).click()
    const firings = page.getByRole('dialog')
    await expect(firings.getByRole('heading', { name: `Firings — ${name}` })).toBeVisible()
    await expect(page.getByTestId('firing-row').filter({ hasText: 'Started' }).first()).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toBeHidden()

    // Switched off before the test ends: a `* * * * *` schedule left enabled
    // would keep starting sessions on the shared backend for the rest of
    // the run.
    const toggle = page.getByRole('switch', { name: `Enabled for ${name}` })
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'false')
    await expect(sameRow).toContainText('paused')
  })

  test('a looping template runs itself out on one session', async ({ page }) => {
    const templateId = await ensureRealLoopTemplate(page)

    // "Run now" in the template editor is POST /templates/{id}/run, which
    // starts the loop's first iteration (origin ui) exactly as a webhook or
    // a schedule would.
    await page.goto(`/templates/${templateId}`)
    await expect(page.getByRole('heading', { name: REAL_LOOP_TEMPLATE_NAME })).toBeVisible()
    await page.getByRole('button', { name: 'Run now' }).click()
    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}$/)

    await page.goto('/runs/loops')
    const row = page.getByTestId(/^loop-row-/).filter({ hasText: REAL_LOOP_TEMPLATE_NAME })

    // Iteration 1's report has no `done` field, so the engine sends a
    // second prompt on the same session; iteration 2 spends the budget and
    // the loop ends `exhausted`. The tab polls every 10 s, so this waits
    // rather than reloading.
    await expect(row.first()).toContainText('Exhausted', { timeout: 60_000 })
    await expect(row.first()).toContainText('2/2')
    await expect(row.first().getByRole('button', { name: 'Stop' })).toHaveCount(0)

    // Both iterations are runs on the one session the loop kept resuming.
    await row.first().getByRole('button', { name: 'Details' }).click()
    const drawer = page.getByRole('dialog')
    await expect(drawer.getByTestId('loop-iteration')).toHaveCount(2)
    await drawer.getByRole('button', { name: /Iteration 2/ }).click()
    await expect(drawer.getByText(/disk/i).first()).toBeVisible()
  })
})
