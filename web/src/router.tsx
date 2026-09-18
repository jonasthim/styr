import { Outlet, createRootRoute, createRoute, createRouter, redirect } from '@tanstack/react-router'
import { AuthGate } from './components/shell/AuthGate'
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

// Pathless layout route: everything except /login renders inside AuthGate.
const appLayoutRoute = createRoute({
  id: '_app',
  getParentRoute: () => rootRoute,
  component: () => (
    <AuthGate>
      <Outlet />
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
const sessionsRoute = createRoute({ getParentRoute: () => appLayoutRoute, path: '/sessions', component: Sessions })
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
