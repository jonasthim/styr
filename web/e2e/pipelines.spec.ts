// Pipelines (v0.5): the YAML editor with its live graph preview and inline
// validation, starting a pipeline, and the pipeline run page with the live
// graph, the fan-out counter, the retry badge, the step panel and Cancel.
//
// The blocks above the divider drive the msw mock's seeded pipelines - a run
// mid-flight with a retry in progress and a half-done fan-out, states the
// real executor cannot be held in long enough to assert on. The
// "(real backend)" block below drives the real thing end to end: a pipeline
// written in the editor whose second step reads the first one's report, a
// fan-out, a retry, a shared worktree, and a trigger and a schedule that
// start one.
import { test, expect } from '@playwright/test'
import {
  FIXTURE_06_DIAGNOSIS_PHRASE,
  PIPELINE_FAILING_TEMPLATE_NAME,
  PIPELINE_NOTE_TEMPLATE_NAME,
  PIPELINE_REPORT_TEMPLATE_NAME,
  PIPELINE_WORKTREE_FILE_ONE,
  PIPELINE_WORKTREE_FILE_TWO,
  PIPELINE_WRITE_ONE_TEMPLATE_NAME,
  PIPELINE_WRITE_TWO_TEMPLATE_NAME,
  REAL_WORKSPACE_NAME,
  createRealPipeline,
  ensureRealPipelineTemplates,
  ensureRealServiceToken,
  ensureRealWorkspace,
  ensureRealWorktreePipelineTemplates,
  ensureRealWorktreeWorkspaceRef,
  isReal,
  waitForPipelineRun,
} from './helpers/seed'

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
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
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
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
    await page.goto(`/pipelines/${PIPELINE_FIX_CI_ID}`)
    await expect(page.getByTestId('graph-node').first()).toBeVisible()

    await page.getByLabel('Definition').fill(FORWARD_NEEDS_YAML)

    const error = page.getByTestId('yaml-error').first()
    await expect(error).toBeVisible()
    await expect(error).toContainText('Line 7')
    await expect(error).toContainText('verify')
  })

  test('Start opens the run it created', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
    await page.goto(`/pipelines/${PIPELINE_FIX_CI_ID}`)

    // exact: a graph node is a button too, and before a run its label ends
    // in "Not started", which a substring match on "Start" also picks up.
    await page.getByRole('button', { name: 'Start', exact: true }).click()
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
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
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
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)

    await page.getByTestId('graph-node').filter({ hasText: 'triage' }).click()
    const panel = page.getByTestId('step-panel')
    await expect(panel).toBeVisible()
    await expect(panel.getByTestId('run-report')).toContainText('flaky integration test')
    await expect(panel.getByRole('link', { name: 'Open session' })).toBeVisible()
  })

  test('Cancel asks before it stops the run', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)

    await page.getByRole('button', { name: 'Cancel run' }).click()
    const confirm = page.getByRole('dialog')
    await expect(confirm.getByRole('heading', { name: 'Cancel this pipeline run?' })).toBeVisible()
    await confirm.getByRole('button', { name: 'Cancel the run' }).click()

    await expect(page.getByTestId('toast').filter({ hasText: 'Pipeline run cancelled' })).toBeVisible()
    await expect(page.getByTestId('pipeline-run-state')).toContainText('Cancelled')
  })

  test('the Pipelines tab under Runs lists pipeline runs', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
    await page.goto('/runs/pipelines')
    await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible()

    const row = page.getByTestId('pipeline-run-row').filter({ hasText: PIPELINE_FIX_CI_NAME }).first()
    await expect(row).toBeVisible()
    await row.click()
    await expect(page).toHaveURL(/\/pipeline-runs\/.+/)
  })

  test('a step run says which pipeline it belongs to', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
    await page.goto(`/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`)

    await page.getByTestId('step-log-row').filter({ hasText: 'triage' }).getByRole('link', { name: 'Run' }).click()
    await expect(page).toHaveURL(/\/runs\/.+/)
    await expect(page.getByTestId('pipeline-step-chip')).toContainText(`Step of pipeline ${PIPELINE_FIX_CI_NAME}`)
  })

  test('phone layout: the run page keeps the graph inside the screen', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
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
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
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
    test.skip(isReal(testInfo), 'the mock fixture pipelines; the real lane is at the bottom of this file')
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

// --- the real backend ------------------------------------------------------
// One executor, one run engine and one shared SQLite database (workers: 1,
// see playwright.config.ts). Every step these pipelines run is an unattended
// session against the shell fake, whose "[fixture:NN]" marker picks the
// recorded transcript: fixture 06 for a step that reports (a structured
// severity/diagnosis/confidence the next step can read), fixture 05 for a
// step that is meant to fail (an interrupted turn the CLI flags is_error).
test.describe('Pipelines (real backend)', () => {
  test.describe.configure({ mode: 'serial' })

  test.beforeEach(async ({}, testInfo) => {
    test.skip(!isReal(testInfo), 'drives the real backend; the mock lane is above')
  })

  test('a two-step pipeline is written in the editor, and the second step reads the first one’s report', async ({
    page,
  }) => {
    await ensureRealPipelineTemplates(page)
    const name = `e2e-two-step-${Date.now()}`
    // `with:` feeds the first step's own diagnosis into the second step's
    // `.note`, which its template renders into the prompt it sends - the
    // whole point of chaining agents, and what the transcript check below
    // proves actually happened.
    const yaml = [
      `name: ${name}`,
      `workspace: ${REAL_WORKSPACE_NAME}`,
      'timeout: 20m',
      'steps:',
      '  - id: triage',
      `    template: "${PIPELINE_REPORT_TEMPLATE_NAME}"`,
      '  - id: follow-up',
      '    needs: [triage]',
      `    template: "${PIPELINE_NOTE_TEMPLATE_NAME}"`,
      '    with: { note: "{{ .steps.triage.report.diagnosis }}" }',
      '',
    ].join('\n')

    // "New from template" creates a valid skeleton and opens it; the editor
    // is where the definition above is actually written.
    await page.goto('/pipelines')
    await expect(page.getByRole('heading', { name: 'Pipelines' })).toBeVisible()
    await page.getByRole('button', { name: 'New from template' }).click()
    await expect(page).toHaveURL(/\/pipelines\/[0-9a-f-]{36}$/)

    // The workspace is set explicitly: the definition names it by name and
    // the validator rejects a mismatch, and which workspace a fresh pipeline
    // starts on depends on whatever GET /workspaces happens to list first.
    await page.getByRole('combobox', { name: 'Workspace' }).click()
    // exact: every other spec's workspaces are named "styr-e2e-<something>".
    await page.getByRole('option', { name: REAL_WORKSPACE_NAME, exact: true }).click()
    await page.getByLabel('Definition').fill(yaml)

    // The graph is the real validator's answer, not the editor's guess.
    await expect(page.getByTestId('graph-node')).toHaveCount(2)
    await expect(page.getByTestId('pipeline-graph').locator('.react-flow__edge')).toHaveCount(1)
    await expect(page.getByTestId('yaml-error')).toHaveCount(0)

    await page.getByRole('button', { name: 'Save' }).click()
    await expect(page.getByTestId('toast').filter({ hasText: 'Pipeline saved' })).toBeVisible()

    // exact: a graph node is a button too, and before a run its label ends
    // in "Not started", which a substring match on "Start" also picks up.
    await page.getByRole('button', { name: 'Start', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: `Start ${name}` })).toBeVisible()
    await dialog.getByRole('button', { name: 'Start pipeline' }).click()

    await expect(page).toHaveURL(/\/pipeline-runs\/[0-9a-f-]{36}$/)
    await expect(page.getByTestId('pipeline-run-state')).toContainText('Succeeded', { timeout: 90_000 })

    // The second node's panel carries the report its own run produced...
    await page.getByTestId('graph-node').filter({ hasText: 'follow-up' }).click()
    const panel = page.getByTestId('step-panel')
    await expect(panel).toBeVisible()
    await expect(panel.getByTestId('run-report')).toContainText(/Disk on host x at 91%/)

    // ...and its session's transcript carries the first step's diagnosis,
    // because that is what its prompt was rendered from.
    await panel.getByRole('link', { name: 'Open session' }).click()
    await expect(page).toHaveURL(/\/sessions\//)
    await expect(page.getByTestId('transcript')).toContainText(FIXTURE_06_DIAGNOSIS_PHRASE)
  })

  test('a foreach step fans out into one run per item', async ({ page }) => {
    await ensureRealPipelineTemplates(page)
    const name = `e2e-fan-out-${Date.now()}`
    const yaml = [
      `name: ${name}`,
      `workspace: ${REAL_WORKSPACE_NAME}`,
      'timeout: 20m',
      'steps:',
      '  - id: triage',
      `    template: "${PIPELINE_REPORT_TEMPLATE_NAME}"`,
      '  - id: fan',
      '    needs: [triage]',
      `    foreach: '["a","b"]'`,
      `    template: "${PIPELINE_REPORT_TEMPLATE_NAME}"`,
      '',
    ].join('\n')

    const workspaceId = await ensureRealWorkspace(page)
    const pipeline = await createRealPipeline(page, name, workspaceId, yaml)
    const started = await page.request.post(`/api/v1/pipelines/${pipeline.id}/start`, {
      headers: { 'X-Requested-With': 'styr' },
      data: { input: {} },
    })
    expect(started.status()).toBe(202)
    const { pipeline_run_id: runId } = (await started.json()) as { pipeline_run_id: string }

    const view = await waitForPipelineRun(page, runId)
    expect(view.run.state).toBe('success')
    // Two step runs for one node: the fan-out expanded the two-item list.
    expect(view.steps.filter((s) => s.step_id === 'fan')).toHaveLength(2)

    await page.goto(`/pipeline-runs/${runId}`)
    const fan = page.getByTestId('graph-node').filter({ hasText: 'fan' })
    await expect(fan).toContainText('Fan-out')
    await expect(fan).toContainText('2/2')
    await expect(page.getByTestId('step-log-row').filter({ hasText: 'fan' })).toHaveCount(2)
  })

  test('a failing step retries once, then fails the pipeline and skips what follows it', async ({ page }) => {
    await ensureRealPipelineTemplates(page)
    const name = `e2e-retry-${Date.now()}`
    const yaml = [
      `name: ${name}`,
      `workspace: ${REAL_WORKSPACE_NAME}`,
      'timeout: 20m',
      'steps:',
      '  - id: boom',
      `    template: "${PIPELINE_FAILING_TEMPLATE_NAME}"`,
      '    retries: 1',
      '  - id: after',
      '    needs: [boom]',
      `    template: "${PIPELINE_REPORT_TEMPLATE_NAME}"`,
      '',
    ].join('\n')

    const workspaceId = await ensureRealWorkspace(page)
    const pipeline = await createRealPipeline(page, name, workspaceId, yaml)
    const started = await page.request.post(`/api/v1/pipelines/${pipeline.id}/start`, {
      headers: { 'X-Requested-With': 'styr' },
      data: {},
    })
    expect(started.status()).toBe(202)
    const { pipeline_run_id: runId } = (await started.json()) as { pipeline_run_id: string }

    const view = await waitForPipelineRun(page, runId)
    expect(view.run.state).toBe('failed')
    // Two attempts of the failing node: the first, and the one its retry
    // budget paid for.
    expect(view.steps.filter((s) => s.step_id === 'boom').map((s) => s.attempt).sort()).toEqual([1, 2])

    await page.goto(`/pipeline-runs/${runId}`)
    await expect(page.getByTestId('pipeline-run-state')).toContainText('Failed')
    const boom = page.getByTestId('graph-node').filter({ hasText: 'boom' })
    await expect(boom).toContainText('Attempt 2')
    await expect(boom).toHaveAttribute('data-state', 'failed')
    // Nothing downstream of a failed node runs.
    const after = page.getByTestId('graph-node').filter({ hasText: 'after' })
    await expect(after).toHaveAttribute('data-state', 'skipped')
    await expect(page.getByTestId('step-log-row').filter({ hasText: 'boom' })).toHaveCount(2)
  })

  test('two steps sharing a worktree leave both their files in it', async ({ page }) => {
    await ensureRealWorktreePipelineTemplates(page)
    const { id: workspaceId, name: workspaceName } = await ensureRealWorktreeWorkspaceRef(page)
    const name = `e2e-shared-worktree-${Date.now()}`
    const yaml = [
      `name: ${name}`,
      `workspace: ${workspaceName}`,
      'timeout: 20m',
      'steps:',
      '  - id: first',
      `    template: "${PIPELINE_WRITE_ONE_TEMPLATE_NAME}"`,
      '  - id: second',
      '    needs: [first]',
      `    template: "${PIPELINE_WRITE_TWO_TEMPLATE_NAME}"`,
      '    worktree: shared',
      '',
    ].join('\n')

    const pipeline = await createRealPipeline(page, name, workspaceId, yaml)
    const started = await page.request.post(`/api/v1/pipelines/${pipeline.id}/start`, {
      headers: { 'X-Requested-With': 'styr' },
      data: {},
    })
    expect(started.status()).toBe(202)
    const { pipeline_run_id: runId } = (await started.json()) as { pipeline_run_id: string }

    const view = await waitForPipelineRun(page, runId)
    expect(view.run.state).toBe('success')

    await page.goto(`/pipeline-runs/${runId}`)
    await page.getByTestId('graph-node').filter({ hasText: 'second' }).click()
    const panel = page.getByTestId('step-panel')
    await expect(panel).toBeVisible()
    await panel.getByRole('link', { name: 'Open session' }).click()
    await expect(page).toHaveURL(/\/sessions\//)

    // The second step ran in the first step's worktree rather than a fresh
    // one from base, so its Review tab sees both files - the proof that
    // "worktree: shared" really continued the same checkout.
    await expect(page.getByTestId('session-view')).toBeVisible()
    await page.getByRole('tab', { name: 'Review' }).click()
    const files = page.getByTestId('review-file')
    await expect(files).toHaveCount(2)
    await expect(files.filter({ hasText: PIPELINE_WORKTREE_FILE_ONE })).toHaveCount(1)
    await expect(files.filter({ hasText: PIPELINE_WORKTREE_FILE_TWO })).toHaveCount(1)
  })

  test('a trigger starts a pipeline and its delivery links to the pipeline run', async ({ page }) => {
    await ensureRealServiceToken(page)
    await ensureRealPipelineTemplates(page)
    const workspaceId = await ensureRealWorkspace(page)
    const stamp = Date.now()
    const yaml = [
      `name: e2e-trigger-pipeline-${stamp}`,
      `workspace: ${REAL_WORKSPACE_NAME}`,
      'timeout: 20m',
      'steps:',
      '  - id: triage',
      `    template: "${PIPELINE_REPORT_TEMPLATE_NAME}"`,
      '',
    ].join('\n')
    const pipeline = await createRealPipeline(page, `e2e-trigger-pipeline-${stamp}`, workspaceId, yaml)

    const triggerName = `Pipeline webhook ${stamp}`
    const createdTrigger = await page.request.post('/api/v1/triggers', {
      headers: { 'X-Requested-With': 'styr' },
      data: { name: triggerName, kind: 'generic', pipeline_id: pipeline.id, cooldown_s: 0, storm_cap_per_hour: 100 },
    })
    expect(createdTrigger.status()).toBe(201)
    const { trigger, secret } = (await createdTrigger.json()) as {
      trigger: { id: string; slug: string; pipeline_id: string | null }
      secret: string
    }
    expect(trigger.pipeline_id).toBe(pipeline.id)

    // The inbound hook is outside /api/v1: the trigger's own secret is the
    // whole credential.
    const res = await page.request.post(`/hooks/${trigger.slug}`, {
      headers: { Authorization: `Bearer ${secret}` },
      data: { title: 'disk filling up' },
    })
    expect(res.status()).toBe(202)
    const delivery = (await res.json()) as { status: string; run_id?: string }
    expect(delivery.status).toBe('accepted')
    // A pipeline run is not a run row, so the delivery records it behind the
    // "pr:" prefix (internal/triggers' PipelineRunRefPrefix).
    expect(delivery.run_id).toMatch(/^pr:[0-9a-f-]{36}$/)
    const pipelineRunId = delivery.run_id!.slice('pr:'.length)

    await page.goto('/triggers')
    const row = page.getByTestId(`trigger-row-${trigger.id}`)
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: 'Deliveries' }).click()
    const drawer = page.getByRole('dialog')
    await expect(drawer.getByRole('heading', { name: `Deliveries — ${triggerName}` })).toBeVisible()
    await page.getByTestId('delivery-row').first().getByRole('link', { name: 'View pipeline run' }).click()

    await expect(page).toHaveURL(new RegExp(`/pipeline-runs/${pipelineRunId}$`))
    await expect(page.getByTestId('pipeline-graph')).toBeVisible()
    await expect(page.getByTestId('pipeline-run-state')).toContainText(/Running|Succeeded/)
    await waitForPipelineRun(page, pipelineRunId)
  })

  test('Run now on a schedule bound to a pipeline opens the pipeline run', async ({ page }) => {
    await ensureRealServiceToken(page)
    await ensureRealPipelineTemplates(page)
    const workspaceId = await ensureRealWorkspace(page)
    const stamp = Date.now()
    const yaml = [
      `name: e2e-schedule-pipeline-${stamp}`,
      `workspace: ${REAL_WORKSPACE_NAME}`,
      'timeout: 20m',
      'steps:',
      '  - id: triage',
      `    template: "${PIPELINE_REPORT_TEMPLATE_NAME}"`,
      '',
    ].join('\n')
    const pipeline = await createRealPipeline(page, `e2e-schedule-pipeline-${stamp}`, workspaceId, yaml)

    // A cron that will not come round during the run: "Run now" is what
    // fires this one, and a schedule left ticking would keep starting
    // sessions on the shared backend.
    const name = `Pipeline schedule ${stamp}`
    const createdSchedule = await page.request.post('/api/v1/schedules', {
      headers: { 'X-Requested-With': 'styr' },
      data: { name, pipeline_id: pipeline.id, cron: '0 4 1 1 *', shared: true },
    })
    expect(createdSchedule.status()).toBe(201)

    await page.goto('/schedules')
    const row = page.getByTestId(/^schedule-row-/).filter({ hasText: name })
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: 'Run now' }).click()

    await expect(page.getByTestId('toast').filter({ hasText: 'Pipeline run started' })).toBeVisible()
    await expect(page).toHaveURL(/\/pipeline-runs\/[0-9a-f-]{36}$/)
    await expect(page.getByTestId('pipeline-graph')).toBeVisible()
    const runId = page.url().slice(page.url().lastIndexOf('/') + 1)
    await waitForPipelineRun(page, runId)
  })
})
