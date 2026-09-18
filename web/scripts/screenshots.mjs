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
