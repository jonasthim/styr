// 48x14 px activity sparkline for a session row (Task 19). Purely a
// rendering component: SessionRow decides whether `values` are real
// tool-events-per-minute buckets or a `flat` fallback, per the card's
// "derive from the session's events query if loaded, else from
// session.tokens_out fallback: render flat".
const WIDTH = 48
const HEIGHT = 14
const PAD = 1.5

// Purely decorative: the row it lives in already carries a full aria-label
// (title, state, workspace, stats), so this stays out of the accessibility
// tree rather than being separately announced while browsing the row.
export function Sparkline({ values, flat = false, className }: { values: number[]; flat?: boolean; className?: string }) {
  if (flat || values.length === 0) {
    const y = HEIGHT / 2
    return (
      <svg width={WIDTH} height={HEIGHT} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} aria-hidden className={className}>
        <line x1={PAD} y1={y} x2={WIDTH - PAD} y2={y} className="stroke-fg-muted" strokeWidth={1} strokeLinecap="round" />
      </svg>
    )
  }

  const max = Math.max(1, ...values)
  const stepX = values.length > 1 ? (WIDTH - PAD * 2) / (values.length - 1) : 0
  const points = values
    .map((v, i) => {
      const x = PAD + i * stepX
      const y = PAD + (1 - v / max) * (HEIGHT - PAD * 2)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')

  return (
    <svg width={WIDTH} height={HEIGHT} viewBox={`0 0 ${WIDTH} ${HEIGHT}`} aria-hidden className={className}>
      <polyline points={points} fill="none" className="stroke-fg-muted" strokeWidth={1.25} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
