import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { Workspace } from '../api/types'

/**
 * Refetches the workspaces list every two seconds while any workspace is
 * still cloning. Live `workspace.state` frames normally update the cache
 * first, but a clone can finish before the created row lands in the cache
 * (fast local clones) or a frame can be missed during a reconnect; polling
 * closes both gaps and stops as soon as nothing is cloning.
 */
export function useCloningPoll(workspaces: Workspace[] | undefined) {
  const queryClient = useQueryClient()
  const cloning = workspaces?.some((w) => w.state === 'cloning') ?? false
  useEffect(() => {
    if (!cloning) return
    const id = window.setInterval(() => {
      void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    }, 2000)
    return () => window.clearInterval(id)
  }, [cloning, queryClient])
}
