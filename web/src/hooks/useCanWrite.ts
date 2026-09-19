// T64: viewer is read-only everywhere except its own /me routes (prefs, API
// tokens, Claude token - see internal/api/middleware.go's writerGuard).
// Every other button, switch and dialog that changes server state checks
// this hook and hides or disables itself for a viewer, matching what the
// backend's writer guard would refuse anyway.
import { useMe } from './useMe'

/** True once `me` has loaded and its role is not "viewer" - false while
 * loading (so a write control does not flash enabled before `me` resolves)
 * and false for a viewer. */
export function useCanWrite(): boolean {
  const { data: me } = useMe()
  return me?.role !== undefined && me.role !== 'viewer'
}
