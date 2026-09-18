import { defineConfig, devices } from '@playwright/test'

// Playwright starts every entry in `webServer` for every run regardless of
// which --project filters are passed, so a plain `real` webServer entry
// would try (and fail) to boot `make dev-backend` while the backend lane
// hasn't reached Task 14 yet. Instead this reads the --project flags off
// argv and only starts the server(s) those projects actually need; with no
// --project filter (a plain `npx playwright test`, as the integration wave
// runs once the real backend exists) both start.
const requestedProjects = process.argv.flatMap((arg, i, argv) => {
  if (arg.startsWith('--project=')) return [arg.slice('--project='.length)]
  if (arg === '--project') return argv[i + 1] ? [argv[i + 1]] : []
  return []
})

function wants(prefix: 'mock' | 'real'): boolean {
  if (requestedProjects.length === 0) return true
  return requestedProjects.some((p) => p.startsWith(prefix))
}

interface WebServerEntry {
  command: string
  url: string
  reuseExistingServer: boolean
  timeout: number
  cwd?: string
  env?: Record<string, string>
}

// Port 5173 is Vite's (and dev:mock's) default, matching the dev-server proxy
// setup in vite.config.ts. On this workstation it collides with an unrelated
// project's --strictPort dev server that is always running, so the mock
// webServer pins an alternate port here rather than changing the shared
// default (`npm run dev`/`npm run dev:mock` run manually still use 5173).
const MOCK_PORT = 5190

const webServer: WebServerEntry[] = []
if (wants('mock')) {
  webServer.push({
    command: `npm run dev:mock -- --host 127.0.0.1 --port ${MOCK_PORT} --strictPort`,
    url: `http://127.0.0.1:${MOCK_PORT}`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  })
}
if (wants('real')) {
  webServer.push({
    command: 'make dev-backend',
    cwd: '..',
    env: { STYR_CONFIG: 'dev.config.yaml' },
    url: 'http://127.0.0.1:8080/healthz',
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  })
}

const desktopViewport = { width: 1280, height: 800 }
const phoneViewport = { width: 390, height: 844 }

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: 'list',
  webServer,
  use: {
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'mock-desktop',
      use: { ...devices['Desktop Chrome'], baseURL: `http://127.0.0.1:${MOCK_PORT}`, viewport: desktopViewport },
    },
    {
      name: 'mock-phone',
      use: { ...devices['Desktop Chrome'], baseURL: `http://127.0.0.1:${MOCK_PORT}`, viewport: phoneViewport },
    },
    {
      name: 'real-desktop',
      use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:8080', viewport: desktopViewport },
    },
    {
      name: 'real-phone',
      use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:8080', viewport: phoneViewport },
    },
  ],
})
