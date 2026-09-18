import { Outlet, createRootRoute, createRoute, createRouter, redirect } from '@tanstack/react-router'
import { AuthGate } from './components/shell/AuthGate'
import { Shell } from './components/shell/Shell'
import { Login } from './pages/Login'
import { Inbox } from './pages/Inbox'
import { Sessions } from './pages/Sessions'
import { SessionDetail } from './pages/SessionDetail'
import { Workspaces } from './pages/Workspaces'
import { Settings } from './pages/Settings'
import { Profile } from './pages/Profile'
import { Onboarding } from './components/onboarding/Onboarding'
import { Runs } from './pages/Runs'
import { RunDetail } from './pages/RunDetail'
import { Triggers } from './pages/Triggers'
import { TemplateEditor } from './pages/TemplateEditor'
import { Schedules } from './pages/Schedules'
import { Loops } from './pages/Loops'
import { Costs } from './pages/Costs'
import { Pipelines } from './pages/Pipelines'
import { PipelineEditor } from './pages/PipelineEditor'
import { PipelineRuns } from './pages/PipelineRuns'
import { PipelineRunDetail } from './pages/PipelineRunDetail'
import type { LoopState, RunOutcome } from './api/types'

const rootRoute = createRootRoute({ component: Outlet })

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: Login,
})

// Pathless layout route: everything except /login renders inside AuthGate,
// wrapped by the app Shell (rail/tab bar, palette, shortcuts, theme).
const appLayoutRoute = createRoute({
  id: '_app',
  getParentRoute: () => rootRoute,
  component: () => (
    <AuthGate>
      <Shell>
        <Outlet />
      </Shell>
    </AuthGate>
  ),
})

const indexRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/inbox' })
  },
})

const inboxRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/inbox', component: Inbox })

interface SessionsSearch {
  new?: number
  // List or fleet Gantt, and the Gantt's window in hours (T49). Search
  // params so a Gantt someone is watching is a link they can send.
  view?: 'gantt'
  window?: '1h' | '6h' | '24h'
}

const sessionsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/sessions',
  // "New session" (palette action, `n` shortcut) navigates to
  // /sessions?new=1; Task 19's Sessions page reads this to open the
  // new-session flow. Placeholder for now, but the route already accepts it.
  // (A number, not a string: the default search serializer JSON-stringifies
  // each value, and a string value would round-trip as `new=%221%22`.)
  validateSearch: (search: Record<string, unknown>): SessionsSearch => ({
    new: search.new === 1 ? 1 : undefined,
    view: search.view === 'gantt' ? 'gantt' : undefined,
    window: search.window === '1h' || search.window === '6h' || search.window === '24h' ? search.window : undefined,
  }),
  component: Sessions,
})
interface SessionDetailSearch {
  // Set by ChangesList.tsx when a file row is clicked; SessionDetail.tsx
  // filters the transcript to blocks touching this path (Task 21).
  file?: string
  // Set by the Review tab's file list (T43): the path whose diff replaces the
  // transcript in the main area. A search param rather than local state so a
  // review of one file is a link a reviewer can send someone.
  diff?: string
}

const sessionDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/sessions/$id',
  validateSearch: (search: Record<string, unknown>): SessionDetailSearch => ({
    file: typeof search.file === 'string' ? search.file : undefined,
    diff: typeof search.diff === 'string' ? search.diff : undefined,
  }),
  component: SessionDetail,
})
const workspacesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/workspaces',
  component: Workspaces,
})
const settingsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/settings', component: Settings })
const profileRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/profile', component: Profile })

interface RunsSearch {
  // Filter chip state (Runs.tsx); a search param rather than local state so
  // a filtered view is a link someone can share or bookmark.
  outcome?: RunOutcome
}

const runsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/runs',
  validateSearch: (search: Record<string, unknown>): RunsSearch => ({
    outcome: typeof search.outcome === 'string' ? (search.outcome as RunOutcome) : undefined,
  }),
  component: Runs,
})
// The two static children of /runs are declared before /runs/$id for
// readability only - TanStack ranks a static segment above a param one
// whatever the order, so /runs/loops is never read as a run id.
interface LoopsSearch {
  // Filter chip state (Loops.tsx), a search param for the same reason the
  // runs list's outcome filter is one: a filtered view is a link.
  state?: LoopState
}

const loopsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/runs/loops',
  validateSearch: (search: Record<string, unknown>): LoopsSearch => ({
    state: typeof search.state === 'string' ? (search.state as LoopState) : undefined,
  }),
  component: Loops,
})
const costsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/runs/costs', component: Costs })
const pipelineRunsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/runs/pipelines',
  component: PipelineRuns,
})
const runDetailRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/runs/$id', component: RunDetail })

const schedulesRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/schedules', component: Schedules })

// v0.5 pipelines: the list, the YAML editor, and a pipeline run's own page.
// /pipeline-runs is a top-level path rather than a child of /runs because a
// pipeline run is not a run - it owns several of them.
const pipelinesRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/pipelines', component: Pipelines })
const pipelineEditorRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/pipelines/$id',
  component: PipelineEditor,
})
const pipelineRunDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/pipeline-runs/$id',
  component: PipelineRunDetail,
})

interface TriggersSearch {
  // Which of the Triggers|Templates tabs is active (Triggers.tsx).
  tab?: 'triggers' | 'templates'
}

const triggersRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/triggers',
  validateSearch: (search: Record<string, unknown>): TriggersSearch => ({
    tab: search.tab === 'templates' ? 'templates' : undefined,
  }),
  component: Triggers,
})
const templateEditorRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/templates/$id',
  component: TemplateEditor,
})
// Task 22 onboarding: shown after first login when `me.claude_token.present`
// is false. The redirect itself lives on the Inbox route (outside this
// card's files); this route just renders the three-step card when reached.
const welcomeRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/welcome', component: Onboarding })

const routeTree = rootRoute.addChildren([
  loginRoute,
  appLayoutRoute.addChildren([
    indexRoute,
    inboxRoute,
    sessionsRoute,
    sessionDetailRoute,
    workspacesRoute,
    settingsRoute,
    profileRoute,
    welcomeRoute,
    runsRoute,
    loopsRoute,
    costsRoute,
    pipelineRunsRoute,
    runDetailRoute,
    schedulesRoute,
    pipelinesRoute,
    pipelineEditorRoute,
    pipelineRunDetailRoute,
    triggersRoute,
    templateEditorRoute,
  ]),
])

export const router = createRouter({ routeTree })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
