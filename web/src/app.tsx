import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { ApiError } from './api/client'
import { router } from './router'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 4xx is the server's answer, not a blip: retrying it only delays
      // the UI settling on what the server said. It matters for the review
      // reads, which answer 422 for the rest of the session's life once its
      // worktree is discarded - and a query that keeps retrying keeps
      // serving its last successful data, so the diff would linger on
      // screen. Network and 5xx failures keep the default three retries.
      retry: (failureCount, error) =>
        !(error instanceof ApiError && error.status >= 400 && error.status < 500) && failureCount < 3,
    },
  },
})

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
