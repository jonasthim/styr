// The seeded loop, split out of ./schedulesHandlers so ./triggersHandlers
// can resolve a run's loop for GET /runs/{id}'s RunView without importing
// the module that imports it back. Leaf module: types only, no other mock
// imports.
import type { Loop } from '../api/types'

export const TEMPLATE_LOOP_ID = 'tmpl-loop-ci'
export const LOOP_ID = 'loop-ci-1'
export const LOOP_SESSION_ID = 'sess-loop-ci-1'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

// One loop mid-flight: iteration 2 of 3 on the session it keeps resuming,
// started by hand rather than by a trigger. schedulesHandlers.ts seeds the
// template, the session and the two runs that go with it.
export const loops: Loop[] = [
  {
    id: LOOP_ID,
    template_id: TEMPLATE_LOOP_ID,
    session_id: LOOP_SESSION_ID,
    origin: 'ui',
    origin_ref: '',
    until_field: 'done',
    max_iterations: 3,
    iteration: 2,
    state: 'running',
    created_at: iso(34),
    updated_at: iso(4),
  },
]

/** The loop a run belongs to, or null - a run outside a loop carries '',
 * not null (the handler serves a plain Go string). */
export function findLoop(id: string): Loop | null {
  if (!id) return null
  return loops.find((l) => l.id === id) ?? null
}
