// 8px outcome dot for a run row/header (plan: "outcome glyphs: running
// pulsing, success green, failed red, timeout muted, needs_human amber").
// Same pulse technique as components/sessions/StateGlyph.tsx, duplicated
// rather than imported so this stays a self-contained runs/ component.
import type { RunOutcome } from '../../api/types'
import { OUTCOME_DOT_CLASS } from './outcome'

const PULSE_KEYFRAMES_ID = 'styr-run-glyph-pulse'

function ensurePulseKeyframes() {
  if (typeof document === 'undefined' || document.getElementById(PULSE_KEYFRAMES_ID)) return
  const style = document.createElement('style')
  style.id = PULSE_KEYFRAMES_ID
  style.textContent = `@keyframes styr-run-glyph-pulse { 0%, 100% { opacity: 1 } 50% { opacity: 0.4 } }`
  document.head.appendChild(style)
}

export function OutcomeGlyph({ outcome, className }: { outcome: RunOutcome; className?: string }) {
  if (outcome === 'running') ensurePulseKeyframes()

  if (outcome === 'timeout') {
    return <span aria-hidden className={`h-2 w-2 shrink-0 rounded-full border border-fg-muted ${className ?? ''}`} />
  }

  return (
    <span
      aria-hidden
      className={`h-2 w-2 shrink-0 rounded-full ${OUTCOME_DOT_CLASS[outcome]} ${className ?? ''}`}
      style={outcome === 'running' ? { animation: 'styr-run-glyph-pulse 1.2s ease-in-out infinite' } : undefined}
    />
  )
}
