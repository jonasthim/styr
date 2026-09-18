// Shared outcome -> label/tone/colour mapping for the Runs list, run detail
// header and Triggers deliveries drawer's "View run" links. Colours follow
// the plan's glyph spec: "running pulsing, success green, failed red,
// timeout muted, needs_human amber".
import type { BadgeTone } from '../ui'
import type { RunOutcome } from '../../api/types'

export const OUTCOME_LABEL: Record<RunOutcome, string> = {
  running: 'Running',
  success: 'Success',
  failed: 'Failed',
  timeout: 'Timed out',
  needs_human: 'Needs you',
}

export const OUTCOME_TONE: Record<RunOutcome, BadgeTone> = {
  running: 'running',
  success: 'running',
  failed: 'failed',
  timeout: 'neutral',
  needs_human: 'attention',
}

export const OUTCOME_DOT_CLASS: Record<RunOutcome, string> = {
  running: 'bg-state-running',
  success: 'bg-state-running',
  failed: 'bg-state-failed',
  timeout: 'bg-state-idle',
  needs_human: 'bg-state-attention',
}
