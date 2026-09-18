import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { router } from './router'

const queryClient = new QueryClient()

// Mock mode only: lets e2e specs force a refetch (e.g. of ['me'] right after
// toggling the mock's logged-out flag) without a hard page reload, which
// would otherwise reset the mock handlers' module-level state along with
// the rest of the page's JS. Never present in a production build.
if (import.meta.env.VITE_MOCK === '1') {
  ;(window as unknown as { __queryClient: QueryClient }).__queryClient = queryClient
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
