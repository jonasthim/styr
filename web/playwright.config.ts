import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig, devices } from '@playwright/test'

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

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
const MOCK_PORT = Number(process.env.STYR_MOCK_PORT ?? '5190')

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
  // `make dev-backend` serves the SPA from the embedded dist/ directory
  // (web/embed.go), not from Vite, so the bundle has to be rebuilt before
  // the backend starts or the real projects would exercise a stale UI.
  // reuseExistingServer is always false (not just outside CI) and each run
  // gets its own STYR_DATA_DIR so seeded state (workspaces, sessions,
  // approvals, the e2e Claude token) never leaks between runs.
  //
  // STYR_CLAUDE_BIN overrides dev.config.yaml's relative
  // ./testdata/fake-claude/fake-claude.sh with the absolute path to the same
  // script. A session on a worktree-enabled workspace runs its process with
  // cwd set to the worktree (internal/sessions/service.go's sessionCwd), and
  // os/exec resolves a relative program name against that cwd, so the
  // relative form only ever worked for sessions running in the repo checkout
  // itself. The fake resolves its own fixture directory from $0, so an
  // absolute path fixes fixture lookup from any cwd too.
  //
  // STYR_MAX_OPEN_SESSIONS raises dev.config.yaml's default of 4: the shell
  // fake (testdata/fake-claude/fake-claude.sh) never exits on its own after
  // a turn (it loops waiting for more stdin), so every seeded session's
  // harness process - and the scheduler slot it holds - stays alive for the
  // rest of the run unless a test explicitly closes it. The suite seeds
  // more than 4 sessions across its files, and internal/sessions/service.go's
  // slots.acquire blocks (not errors) once slots run out, which otherwise
  // hangs POST /sessions until Playwright's own test timeout.
  webServer.push({
    command: 'npm --prefix web run build && make dev-backend',
    cwd: '..',
    env: {
      STYR_CONFIG: 'dev.config.yaml',
      STYR_DATA_DIR: `data/e2e-${process.pid}`,
      STYR_MAX_OPEN_SESSIONS: '100',
      STYR_CLAUDE_BIN: path.join(repoRoot, 'testdata', 'fake-claude', 'fake-claude.sh'),
      // STYR_CODEX_BIN points the Codex harness at its own shell fake, for the
      // same reason and in the same absolute form as STYR_CLAUDE_BIN above:
      // `codex exec` runs with the session's cwd (its worktree, or the
      // workspace checkout), so a relative program name would not resolve, and
      // the fake finds its fixture directory from $0.
      STYR_CODEX_BIN: path.join(repoRoot, 'testdata', 'fake-codex', 'fake-codex.sh'),
    },
    url: 'http://127.0.0.1:8080/healthz',
    reuseExistingServer: false,
    timeout: 120_000,
  })
}

const desktopViewport = { width: 1280, height: 800 }
const phoneViewport = { width: 390, height: 844 }

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  // The real projects share one backend process and one SQLite database
  // (there is no per-test reset, unlike the mock's per-page module state -
  // see e2e/helpers/seed.ts), so every real-mode test has to run one at a
  // time against that shared, mutable state: workers: 1 serializes the
  // whole run whenever a real project is requested (including a plain
  // `npx playwright test` with no --project filter, which starts both).
  workers: wants('real') ? 1 : undefined,
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
