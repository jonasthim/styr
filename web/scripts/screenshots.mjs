#!/usr/bin/env node
// Captures the README/docs screenshots against the mock (MSW) frontend, the
// same way `npm run dev:mock` runs manually. Standalone script rather than a
// Playwright test/spec: another agent owns web/e2e/*.spec.ts and
// playwright.config.ts while this task is in flight, and those specs are
// wired to a different mock port (see playwright.config.ts's MOCK_PORT
// comment) that this script must not collide with.
//
// Usage: node web/scripts/screenshots.mjs   (run from web/, or via `npm run
// screenshots`, which already sets the cwd)
import { spawn } from 'node:child_process'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright-core'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(__dirname, '..')
const outDir = path.resolve(webRoot, '..', 'docs', 'screenshots')

const HOST = '127.0.0.1'
const PORT = 5189
const BASE_URL = `http://${HOST}:${PORT}`

// The fixed mock session id seeded by web/src/mocks/handlers.ts
// (TOOL_FIXTURE_SESSION_ID): a Read tool call followed by a text reply.
const FIXTURE_SESSION_ID = '00000000-0000-4000-8000-000000000005'

// The seeded, still-running pipeline run (web/src/mocks/pipelinesState.ts's
// PIPELINE_RUN_RUNNING_ID): the plan's own fix-ci example, mid-flight.
const PIPELINE_RUN_RUNNING_ID = 'prun-running-1'

function waitForServer(url, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs
  return new Promise((resolve, reject) => {
    const attempt = async () => {
      try {
        const res = await fetch(url)
        if (res.ok || res.status < 500) {
          resolve()
          return
        }
      } catch {
        // server not up yet
      }
      if (Date.now() > deadline) {
        reject(new Error(`timed out waiting for ${url}`))
        return
      }
      setTimeout(attempt, 300)
    }
    void attempt()
  })
}

async function main() {
  await mkdir(outDir, { recursive: true })

  console.log(`starting dev:mock on ${HOST}:${PORT}...`)
  const server = spawn(
    'npm',
    ['run', 'dev:mock', '--', '--host', HOST, '--port', String(PORT), '--strictPort'],
    { cwd: webRoot, stdio: 'inherit', env: { ...process.env, VITE_MOCK: '1' } },
  )

  let serverExited = false
  server.on('exit', () => {
    serverExited = true
  })

  const cleanup = () => {
    if (!serverExited && server.pid) {
      server.kill('SIGTERM')
    }
  }
  process.on('exit', cleanup)

  try {
    await waitForServer(BASE_URL)
    console.log('dev:mock is up, launching browser...')

    const browser = await chromium.launch()
    try {
      // Dark theme is the default palette (no data-theme attribute needed;
      // see web/src/styles/tokens.css), so nothing special is set here.

      // 1. Inbox, phone viewport.
      {
        const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
        await page.goto(`${BASE_URL}/inbox`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid="needs-you-list"], [data-testid="fyi-list"]', {
          timeout: 15_000,
        })
        await page.screenshot({ path: path.join(outDir, 'inbox-phone.png') })
        await page.close()
        console.log('captured inbox-phone.png (390x844)')
      }

      // 2. Sessions list, desktop viewport.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/sessions`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid^="session-group-"]', { timeout: 15_000 })
        // The left rail (Rail.tsx) expands from 56px to 220px on hover as a
        // fixed overlay; move the mouse away from it and let the 150ms
        // width transition settle so the screenshot shows the collapsed,
        // icons-only rail rather than covering the content underneath it.
        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'sessions.png') })
        await page.close()
        console.log('captured sessions.png (1280x800)')
      }

      // 3. Session view, desktop viewport: expand the tool block, Activity
      // tab is already the default-active tab in SidePanel.tsx.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/sessions/${FIXTURE_SESSION_ID}`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid="session-view"]', { timeout: 15_000 })

        const toolHeader = page.getByRole('button', { expanded: false }).filter({ hasText: 'Read' })
        await toolHeader.first().click()
        await page.waitForSelector('[data-testid="tool-result"]', { timeout: 15_000 })

        // Activity tab is the default; just make sure it rendered its
        // content (a span for the fixture's Read call) before capturing.
        await page.waitForSelector('[data-testid="span"]', { timeout: 15_000 })

        // See the sessions.png comment above: settle the hover-expanding
        // rail before capturing.
        await page.mouse.move(900, 700)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'session.png') })
        await page.close()
        console.log('captured session.png (1280x800)')
      }

      // 4. Runs list, desktop viewport: every unattended run a trigger
      // started (RunRow.tsx), seeded by web/src/mocks/triggersHandlers.ts.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/runs`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid="run-row"]', { timeout: 15_000 })

        // See the sessions.png comment above: settle the hover-expanding
        // rail before capturing.
        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'runs.png') })
        await page.close()
        console.log('captured runs.png (1280x800)')
      }

      // 5. Triggers list, desktop viewport: the Triggers tab (default tab
      // of TriggerRow.tsx's table), also seeded by triggersHandlers.ts.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/triggers`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid^="trigger-row-"]', { timeout: 15_000 })

        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'triggers.png') })
        await page.close()
        console.log('captured triggers.png (1280x800)')
      }

      // 6. Session view with the Review tab open, desktop viewport: the
      // fixture session's seeded diff (web/src/mocks/reviewState.ts), a
      // modified file (internal/sessions/service.go) selected so its diff
      // shows in the main pane, side panel switched to the Review tab.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        const diffPath = 'internal/sessions/service.go'
        await page.goto(
          `${BASE_URL}/sessions/${FIXTURE_SESSION_ID}?diff=${encodeURIComponent(diffPath)}`,
          { waitUntil: 'networkidle' },
        )
        await page.waitForSelector('[data-testid="session-view"]', { timeout: 15_000 })
        await page.getByRole('tab', { name: 'Review' }).click()
        await page.waitForSelector('[data-testid="diff-view"]', { timeout: 15_000 })
        await page.waitForSelector('[data-testid="diff-hunk"]', { timeout: 15_000 })

        // See the sessions.png comment above: settle the hover-expanding
        // rail before capturing.
        await page.mouse.move(900, 700)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'review.png') })
        await page.close()
        console.log('captured review.png (1280x800)')
      }

      // 7. Schedules list, desktop viewport: seeded schedules
      // (web/src/mocks/schedulesHandlers.ts), one enabled with recent
      // firings, one disabled.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/schedules`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid^="schedule-row-"]', { timeout: 15_000 })

        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'schedules.png') })
        await page.close()
        console.log('captured schedules.png (1280x800)')
      }

      // 8. Fleet Gantt, desktop viewport: the Sessions page's Gantt view.
      // window=24h (rather than the default 1h) so the mock's older seeded
      // sessions (web/src/mocks/sessionsState.ts) still fall inside the
      // requested window and render a lane.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/sessions?view=gantt&window=24h`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid="fleet-gantt"]', { timeout: 15_000 })
        await page.waitForSelector('[data-testid="gantt-lane"]', { timeout: 15_000 })

        // See the sessions.png comment above: settle the hover-expanding
        // rail before capturing.
        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'gantt.png') })
        await page.close()
        console.log('captured gantt.png (1280x800)')
      }

      // 9. Cost dashboard, desktop viewport: the Runs > Costs tab, seeded
      // with 30 days of cost data (schedulesHandlers.ts).
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/runs/costs`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid="cost-tile"]', { timeout: 15_000 })

        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'costs.png') })
        await page.close()
        console.log('captured costs.png (1280x800)')
      }

      // 10. Pipelines list, desktop viewport: the seeded fix-ci and
      // release-notes pipelines (web/src/mocks/pipelinesState.ts).
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/pipelines`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid^="pipeline-row-"]', { timeout: 15_000 })

        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'pipelines.png') })
        await page.close()
        console.log('captured pipelines.png (1280x800)')
      }

      // 11. Pipeline run, desktop viewport: the seeded still-running fix-ci
      // run, so the graph shows a mix of success/running/pending nodes.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/pipeline-runs/${PIPELINE_RUN_RUNNING_ID}`, { waitUntil: 'networkidle' })
        await page.waitForSelector('[data-testid="pipeline-run-state"]', { timeout: 15_000 })
        await page.waitForSelector('[data-testid="pipeline-graph"]', { timeout: 15_000 })
        await page.waitForSelector('[data-testid="graph-node"]', { timeout: 15_000 })

        // See the sessions.png comment above: settle the hover-expanding
        // rail before capturing.
        await page.mouse.move(900, 400)
        await page.waitForTimeout(250)
        await page.screenshot({ path: path.join(outDir, 'pipeline-run.png') })
        await page.close()
        console.log('captured pipeline-run.png (1280x800)')
      }

      // 12. New-session dialog, desktop viewport, with the harness select
      // open: the v1.0 second-harness choice (NewSessionDialog.tsx), same
      // trigger web/e2e/session.spec.ts uses to reach it.
      {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
        await page.goto(`${BASE_URL}/sessions?new=1`, { waitUntil: 'networkidle' })
        const dialog = page.getByRole('dialog')
        await dialog.waitFor({ state: 'visible', timeout: 15_000 })
        await dialog.getByRole('combobox', { name: 'Harness' }).click()
        await page.waitForSelector('[role="option"]', { timeout: 15_000 })
        await page.screenshot({ path: path.join(outDir, 'harness.png') })
        await page.close()
        console.log('captured harness.png (1280x800)')
      }
    } finally {
      await browser.close()
    }
  } finally {
    cleanup()
  }

  console.log(`screenshots written to ${outDir}`)
}

main().catch((err) => {
  console.error(err)
  process.exitCode = 1
})
