// Step and pipeline-run state, mapped once for the graph, the log and the
// run header. The palette is the one the rest of Styr already uses for run
// outcomes (components/runs/outcome.ts): green for anything that finished
// well or is under way, red for a failure, muted for something that never
// happened.
import type { BadgeTone } from '../ui'
import type { PipelineRunState, StepRun, StepRunState } from '../../api/types'

export const STEP_LABEL: Record<StepRunState, string> = {
  pending: 'Waiting',
  running: 'Running',
  success: 'Done',
  failed: 'Failed',
  skipped: 'Skipped',
  cancelled: 'Cancelled',
}

export const STEP_TONE: Record<StepRunState, BadgeTone> = {
  pending: 'neutral',
  running: 'running',
  success: 'running',
  failed: 'failed',
  skipped: 'neutral',
  cancelled: 'neutral',
}

/** The 3px rail down the left of a node: the whole of a step's state at a
 * glance, in the same idiom the nav rail uses for the active page. */
export const STEP_RAIL_CLASS: Record<StepRunState, string> = {
  pending: 'bg-state-idle/40',
  running: 'bg-state-running',
  success: 'bg-state-running/70',
  failed: 'bg-state-failed',
  skipped: 'bg-state-idle/30',
  cancelled: 'bg-state-idle/60',
}

export const RUN_STATE_LABEL: Record<PipelineRunState, string> = {
  running: 'Running',
  success: 'Succeeded',
  failed: 'Failed',
  cancelled: 'Cancelled',
  timeout: 'Timed out',
}

export const RUN_STATE_TONE: Record<PipelineRunState, BadgeTone> = {
  running: 'running',
  success: 'running',
  failed: 'failed',
  cancelled: 'neutral',
  timeout: 'attention',
}

/** What one node of the graph shows: the state of its newest attempt, how
 * many of a fan-out's items are done, and which attempt it is on. A node
 * with no step runs yet (the editor's preview) has none of it. */
export interface NodeProgress {
  state: StepRunState
  attempt: number
  /** Fan-out items finished / expected; `total` is 1 for a plain step. */
  done: number
  total: number
  /** The step runs behind this node, newest attempt last. */
  steps: StepRun[]
}

const WORST: StepRunState[] = ['success', 'skipped', 'cancelled', 'pending', 'running', 'failed']

/** One node's progress, read off the run's step runs. A fan-out node is
 * summarised across its items (the loudest state wins, so a single failure
 * is visible on a node that is otherwise fine); a retried node reports its
 * newest attempt. */
export function nodeProgress(stepId: string, steps: StepRun[]): NodeProgress | null {
  const mine = steps.filter((s) => s.step_id === stepId)
  if (mine.length === 0) return null

  const byItem = new Map<number, StepRun>()
  for (const step of mine) {
    const current = byItem.get(step.index_in_fanout)
    if (!current || step.attempt >= current.attempt) byItem.set(step.index_in_fanout, step)
  }
  const latest = [...byItem.values()]
  const state = latest.reduce<StepRunState>(
    (worst, step) => (WORST.indexOf(step.state) > WORST.indexOf(worst) ? step.state : worst),
    'success',
  )
  return {
    state,
    attempt: Math.max(...mine.map((s) => s.attempt)),
    done: latest.filter((s) => s.state === 'success').length,
    total: latest.length,
    steps: mine,
  }
}
