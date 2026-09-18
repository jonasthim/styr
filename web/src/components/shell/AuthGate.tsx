import { useEffect, type ReactNode } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useMe } from '../../hooks/useMe'
import { useLiveEvents } from '../../hooks/useLiveEvents'

// Wraps every route but /login. While `me` loads it shows a skeleton shell;
// on 401 it navigates to /login. The live-events connection only opens once
// a user is confirmed, so an unauthenticated visitor never opens an SSE
// connection that will just 401.
export function AuthGate({ children }: { children: ReactNode }) {
  const { data, isLoading, loggedOut } = useMe()
  const navigate = useNavigate()

  useEffect(() => {
    if (!isLoading && loggedOut) {
      void navigate({ to: '/login' })
    }
  }, [isLoading, loggedOut, navigate])

  if (isLoading) {
    return (
      <div
        data-testid="shell-skeleton"
        className="flex min-h-screen items-center justify-center bg-canvas text-[13px] text-fg-muted"
      >
        Loading
      </div>
    )
  }

  if (loggedOut || !data) {
    return null
  }

  return <Authenticated>{children}</Authenticated>
}

function Authenticated({ children }: { children: ReactNode }) {
  useLiveEvents()
  return <>{children}</>
}
