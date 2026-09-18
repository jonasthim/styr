// Pipelines (T54): the YAML editor with its live graph preview and inline
// validation, starting a pipeline, and the pipeline run page with the live
// graph, the fan-out counter, the retry badge, the step panel and Cancel.
// Everything here runs against the msw mock; the real routes land in T57,
// so the real projects skip.
import { test, expect } from '@playwright/test'
import { isReal } from './helpers/seed'

// Fixed mock ids and names (web/src/mocks/pipelinesState.ts) - duplicated as
// literals rather than imported: e2e specs run outside Vite, so they can't
// pull in a mock module (see triggers.spec.ts's own note).
const PIPELINE_FIX_CI_ID = 'pipe-fix-ci'
const PIPELINE_FIX_CI_NAME = 'fix-ci'
const PIPELINE_RUN_RUNNING_ID = 'prun-running-1'

// A definition whose second step needs a step defined below it - the one
// thing a DAG cannot have. The `needs` line is line 7.
const FORWARD_NEEDS_YAML = `name: broken
workspace: styr
steps:
  - id: triage
    template: "CI failure triage"
  - id: fix
    needs: [verify]
    template: "Apply fix"
  - id: verify
    needs: [fix]
    template: "Run tests and report"
`

test.describe('Pipelines editor', () => {
  test('the editor draws the definition as a graph', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto(`/pipelines/${PIPELINE_FIX_CI_ID}`)
    await expect(page.getByRole('heading', { name: PIPELINE_FIX_CI_NAME })).toBeVisible()

    // The plan's fix-ci example: triage -> fix -> verify, and triage ->
    // review-each, so four steps and three dependencies.
    await expect(page.getByTestId('graph-node')).toHaveCount(4)
    // Edges are drawn by React Flow itself, so they are counted by its own
    // class rather than a testid we control.
    await expect(page.getByTestId('pipeline-graph').locator('.react-flow__edge')).toHaveCount(3)

    await expect(page.getByTestId('graph-node').filter({ hasText: 'triage' })).toContainText('CI failure triage')
    // The fan-out step says so on the node, not just in the YAML.
    await expect(page.getByTestId('graph-node').filter({ hasText: 'review-each' })).toContainText('Fan-out')
  })

  test('an invalid definition reports the line it went wrong on', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto(`/pipelines/${PIPELINE_FIX_CI_ID}`)
    await expect(page.getByTestId('graph-node').first()).toBeVisible()

    await page.getByLabel('Definition').fill(FORWARD_NEEDS_YAML)

    const error = page.getByTestId('yaml-error').first()
    await expect(error).toBeVisible()
    await expect(error).toContainText('Line 7')
    await expect(error).toContainText('verify')
  })

  test('Start opens the run it created', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto(`/pipelines/${PIPELINE_FIX_CI_ID}`)

    await page.getByRole('button', { name: 'Start' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: 'Start fix-ci' })).toBeVisible()
    await dialog.getByLabel('Input').fill('{"branch": "main"}')
    await dialog.getByRole('button', { name: 'Start pipeline' }).click()

    await expect(page).toHaveURL(/\/pipeline-runs\/.+/)
    await expect(page.getByTestId('pipeline-graph')).toBeVisible()
  })
})

test.describe('Pipeline run', () => {
  test('the graph shows state, fan-out progress and a retry', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)
    await expect(page.getByRole('heading', { name: PIPELINE_FIX_CI_NAME })).toBeVisible()
    await expect(page.getByTestId('pipeline-run-state')).toContainText('Running')

    // The retried step is on its second attempt and running again.
    const fix = page.getByTestId('graph-node').filter({ hasText: 'fix' }).first()
    await expect(fix).toHaveAttribute('data-state', 'running')
    await expect(fix).toContainText('Attempt 2')

    // One of the two fan-out items is done.
    const fanOut = page.getByTestId('graph-node').filter({ hasText: 'review-each' })
    await expect(fanOut).toContainText('1/2')

    // The log lists one row per step run, the retried step twice.
    await expect(page.getByTestId('step-log-row').filter({ hasText: 'fix' })).toHaveCount(2)
  })

  test('clicking a step opens its report', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)

    await page.getByTestId('graph-node').filter({ hasText: 'triage' }).click()
    const panel = page.getByTestId('step-panel')
    await expect(panel).toBeVisible()
    await expect(panel.getByTestId('run-report')).toContainText('flaky integration test')
    await expect(panel.getByRole('link', { name: 'Open session' })).toBeVisible()
  })

  test('Cancel asks before it stops the run', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)

    await page.getByRole('button', { name: 'Cancel run' }).click()
    const confirm = page.getByRole('dialog')
    await expect(confirm.getByRole('heading', { name: 'Cancel this pipeline run?' })).toBeVisible()
    await confirm.getByRole('button', { name: 'Cancel the run' }).click()

    await expect(page.getByTestId('toast').filter({ hasText: 'Pipeline run cancelled' })).toBeVisible()
    await expect(page.getByTestId('pipeline-run-state')).toContainText('Cancelled')
  })

  test('the Pipelines tab under Runs lists pipeline runs', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto('/runs/pipelines')
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()

    const row = page.getByTestId('pipeline-run-row').filter({ hasText: PIPELINE_FIX_CI_NAME }).first()
    await expect(row).toBeVisible()
    await row.click()
    await expect(page).toHaveURL(/\/pipeline-runs\/.+/)
  })

  test('a step run says which pipeline it belongs to', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)

    await page.getByTestId('step-log-row').filter({ hasText: 'triage' }).getByRole('link', { name: 'Run' }).click()
    await expect(page).toHaveURL(/\/runs\/.+/)
    await expect(page.getByTestId('pipeline-step-chip')).toContainText(`Step of pipeline ${PIPELINE_FIX_CI_NAME}`)
  })

  test('phone layout: the run page keeps the graph inside the screen', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    const isPhone = testInfo.project.name.includes('phone')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)
    await expect(page.getByTestId('pipeline-graph')).toBeVisible()
    await expect(page.getByTestId('graph-node').first()).toBeVisible()

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

test.describe('Pipelines list', () => {
  test('a trigger can run a pipeline instead of a template', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto('/triggers')
    await page.getByRole('button', { name: 'New trigger' }).click()
    const dialog = page.getByRole('dialog')

    await dialog.getByLabel('Name').fill('Nightly CI repair')
    await dialog.getByRole('button', { name: 'Run a pipeline' }).click()
    await dialog.getByRole('combobox', { name: 'Pipeline' }).click()
    await page.getByRole('option', { name: PIPELINE_FIX_CI_NAME }).click()
    await dialog.getByRole('button', { name: 'Create trigger' }).click()

    await expect(dialog.getByRole('heading', { name: 'Webhook ready' })).toBeVisible()
    await dialog.getByRole('button', { name: 'Done' }).click()

    const row = page.getByTestId(/^trigger-row-/).filter({ hasText: 'Nightly CI repair' })
    await expect(row).toContainText(PIPELINE_FIX_CI_NAME)
  })

  test('New from template gives a runnable skeleton', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T57')
    await page.goto('/pipelines')
    await expect(page.getByRole('heading', { name: 'Pipelines' })).toBeVisible()
    await expect(page.getByTestId(/^pipeline-row-/)).toHaveCount(2)

    await page.getByRole('button', { name: 'New from template' }).click()
    await expect(page).toHaveURL(/\/pipelines\/.+/)
    // The skeleton is two sequential steps, so two nodes and one edge.
    await expect(page.getByTestId('graph-node')).toHaveCount(2)
    await expect(page.getByTestId('pipeline-graph').locator('.react-flow__edge')).toHaveCount(1)
  })
})
