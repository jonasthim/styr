// A loop's five states, as a label, a tone and an 8px glyph - the same
// vocabulary runs and sessions use, so "running" looks the same wherever it
// appears.
import type { BadgeTone } from '../ui'
import type { LoopState } from '../../api/types'

export const LOOP_STATE_LABEL: Record<LoopState, string> = {
  running: 'Running',
  done: 'Done',
  exhausted: 'Exhausted',
  failed: 'Failed',
  stopped: 'Stopped',
}

export const LOOP_STATE_TONE: Record<LoopState, BadgeTone> = {
  running: 'running',
  done: 'running',
  exhausted: 'attention',
  failed: 'failed',
  stopped: 'neutral',
}

const DOT_CLASS: Record<LoopState, string> = {
  running: 'bg-state-running',
  done: 'bg-state-running',
  exhausted: 'bg-state-attention',
  failed: 'bg-state-failed',
  stopped: 'bg-state-idle',
}

const PULSE_KEYFRAMES_ID = 'styr-loop-glyph-pulse'

function ensurePulseKeyframes() {
  if (typeof document === 'undefined' || document.getElementById(PULSE_KEYFRAMES_ID)) return
  const style = document.createElement('style')
  style.id = PULSE_KEYFRAMES_ID
  style.textContent = `@keyframes styr-loop-glyph-pulse { 0%, 100% { opacity: 1 } 50% { opacity: 0.4 } }`
  document.head.appendChild(style)
}

export function LoopGlyph({ state }: { state: LoopState }) {
  if (state === 'running') ensurePulseKeyframes()
  return (
    <span
      aria-hidden
      className={`h-2 w-2 shrink-0 rounded-full ${DOT_CLASS[state]}`}
      style={state === 'running' ? { animation: 'styr-loop-glyph-pulse 1.2s ease-in-out infinite' } : undefined}
    />
  )
}
