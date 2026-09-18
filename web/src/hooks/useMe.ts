import { useQuery } from '@tanstack/react-query'
import { q } from '../api/queries'
import { ApiError } from '../api/client'

/** The current user, or null once the me query resolves as unauthenticated. */
export function useMe() {
  const query = useQuery({ ...q.me(), retry: false })
  const loggedOut = query.isError && query.error instanceof ApiError && query.error.status === 401
  return { ...query, loggedOut }
}
