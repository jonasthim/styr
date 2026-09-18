// 8px state dot for a session row (Task 19: "glyph (8 px dot: running pulses
// opacity 1->0.4 over 1.2 s, waiting amber, open muted, closed hollow,
// failed red)"). The label itself is not rendered here - SessionRow folds it
// into the row link's aria-label instead, so this glyph stays aria-hidden
// rather than duplicating an accessible name fragment per row.
import type { SessionState } from '../../api/types'

const PULSE_KEYFRAMES_ID = 'styr-state-glyph-pulse'

// Injected once, lazily, the first time a running glyph mounts. Kept out of
// styles/tokens.css and base.css (this card may only touch files under
// components/sessions/ and pages/Sessions.tsx) - base.css's global
// `prefers-reduced-motion: reduce` rule still neutralises it, since that
// rule targets every element's `animation-duration`/`animation-iteration-
// count`, not just animations it defines itself.
function ensurePulseKeyframes() {
  if (typeof document === 'undefined' || document.getElementById(PULSE_KEYFRAMES_ID)) return
  const style = document.createElement('style')
  style.id = PULSE_KEYFRAMES_ID
  style.textContent = `@keyframes styr-glyph-pulse { 0%, 100% { opacity: 1 } 50% { opacity: 0.4 } }`
  document.head.appendChild(style)
}

export const STATE_LABEL: Record<SessionState, string> = {
  open: 'Idle',
  running: 'Running',
  waiting: 'Needs you',
  closed: 'Closed',
  failed: 'Failed',
}

const FILL_CLASS: Partial<Record<SessionState, string>> = {
  running: 'bg-state-running',
  waiting: 'bg-state-attention',
  open: 'bg-state-idle',
  failed: 'bg-state-failed',
}

export function StateGlyph({ state }: { state: SessionState }) {
  if (state === 'running') ensurePulseKeyframes()

  if (state === 'closed') {
    return <span aria-hidden className="h-2 w-2 shrink-0 rounded-full border border-fg-muted" />
  }

  return (
    <span
      aria-hidden
      className={`h-2 w-2 shrink-0 rounded-full ${FILL_CLASS[state]}`}
      style={state === 'running' ? { animation: 'styr-glyph-pulse 1.2s ease-in-out infinite' } : undefined}
    />
  )
}
