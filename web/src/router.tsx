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
  }),
  component: Sessions,
})
const sessionDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/sessions/$id',
  component: SessionDetail,
})
const workspacesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/workspaces',
  component: Workspaces,
})
const settingsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/settings', component: Settings })
const profileRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/profile', component: Profile })

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
  ]),
])

export const router = createRouter({ routeTree })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
