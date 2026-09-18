// Spend per day, hand-rolled in SVG on the design tokens.
//
// One series, so one colour and no legend: the caption says what is plotted.
// Bars are capped at 24px with a 4px rounded top and a square foot on the
// baseline, the grid is three recessive hairlines, and only the biggest day
// is labelled directly - the axis, the tooltip and the table behind the
// figure carry the rest. That table is the whole point of the <figure>: the
// numbers stay readable when the picture isn't.
import { useRef, useState } from 'react'
import type { CostDay } from '../../api/types'
import { useElementWidth } from '../../hooks/useElementWidth'

const HEIGHT = 168
const PAD_TOP = 18
const PAD_BOTTOM = 18
const AXIS_W = 40
const MAX_BAR_W = 24
const GAP = 2

function money(usd: number): string {
  return `$${usd.toFixed(2)}`
}

function shortDay(day: string): string {
  const d = new Date(`${day}T12:00:00Z`)
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

/** A bar with a 4px rounded cap and a square foot on the baseline. */
function barPath(x: number, y: number, width: number, height: number): string {
  const r = Math.max(0, Math.min(4, width / 2, height))
  return `M ${x} ${y + height} V ${y + r} Q ${x} ${y} ${x + r} ${y} H ${x + width - r} Q ${x + width} ${y} ${x + width} ${y + r} V ${y + height} Z`
}

/** Three round-numbered gridlines, the top one at or just above the peak. */
function gridValues(max: number): number[] {
  if (max <= 0) return [0]
  const step = Math.pow(10, Math.floor(Math.log10(max))) / 2
  const top = Math.ceil(max / step) * step
  return [0, top / 2, top]
}

interface Hover {
  left: number
  top: number
  day: CostDay
}

export function CostChart({ days }: { days: CostDay[] }) {
  const [plotRef, plotWidth] = useElementWidth<HTMLDivElement>()
  const figureRef = useRef<HTMLElement | null>(null)
  const [hover, setHover] = useState<Hover | null>(null)

  const max = Math.max(0.01, ...days.map((d) => d.usd))
  const grid = gridValues(max)
  const ceiling = grid[grid.length - 1] || max
  const plotH = HEIGHT - PAD_TOP - PAD_BOTTOM
  const inner = Math.max(0, plotWidth - AXIS_W)
  const slot = days.length > 0 ? inner / days.length : 0
  const barW = Math.max(2, Math.min(MAX_BAR_W, slot - GAP))
  const peakIndex = days.reduce((best, d, i) => (d.usd > (days[best]?.usd ?? 0) ? i : best), 0)

  const y = (usd: number) => PAD_TOP + plotH - (usd / ceiling) * plotH

  function showHover(event: { clientX: number; clientY: number }, day: CostDay) {
    const rect = figureRef.current?.getBoundingClientRect()
    if (!rect) return
    setHover({
      left: Math.min(Math.max(4, event.clientX - rect.left - 60), Math.max(4, rect.width - 130)),
      top: Math.max(0, event.clientY - rect.top - 52),
      day,
    })
  }

  return (
    <figure ref={figureRef} className="relative m-0" onMouseLeave={() => setHover(null)}>
      <div ref={plotRef} className="w-full">
        <svg width="100%" height={HEIGHT} className="block" aria-hidden>
          {plotWidth > 0 && (
            <>
              {grid.map((value) => (
                <g key={value}>
                  <line
                    x1={AXIS_W}
                    y1={y(value)}
                    x2={plotWidth}
                    y2={y(value)}
                    className="stroke-hairline"
                    strokeWidth={1}
                  />
                  <text
                    x={AXIS_W - 6}
                    y={y(value) + 3}
                    textAnchor="end"
                    fill="var(--fg-muted)"
                    style={{ fontSize: 10, fontVariantNumeric: 'tabular-nums' }}
                  >
                    {money(value)}
                  </text>
                </g>
              ))}

              {days.map((day, i) => {
                const height = Math.max(1, (day.usd / ceiling) * plotH)
                const x = AXIS_W + i * slot + (slot - barW) / 2
                return (
                  <g key={day.day}>
                    <path
                      data-testid="cost-bar"
                      d={barPath(x, y(day.usd), barW, height)}
                      fill="var(--accent)"
                      fillOpacity={hover && hover.day.day !== day.day ? 0.55 : 1}
                    />
                    {/* A full-height hit area, so a 2px bar is still easy to
                        point at. */}
                    <rect
                      x={AXIS_W + i * slot}
                      y={PAD_TOP}
                      width={Math.max(barW, slot)}
                      height={plotH}
                      fill="transparent"
                      className="cursor-default"
                      onMouseEnter={(e) => showHover(e, day)}
                      onMouseMove={(e) => showHover(e, day)}
                    />
                  </g>
                )
              })}

              {/* One direct label: the most expensive day. */}
              {days[peakIndex] && (
                <text
                  x={AXIS_W + peakIndex * slot + slot / 2}
                  y={y(days[peakIndex]!.usd) - 5}
                  textAnchor="middle"
                  fill="var(--fg-secondary)"
                  style={{ fontSize: 10, fontVariantNumeric: 'tabular-nums' }}
                >
                  {money(days[peakIndex]!.usd)}
                </text>
              )}

              {[0, Math.floor(days.length / 2), days.length - 1]
                .filter((i, at, all) => i >= 0 && days[i] && all.indexOf(i) === at)
                .map((i) => (
                  <text
                    key={days[i]!.day}
                    x={AXIS_W + i * slot + slot / 2}
                    y={HEIGHT - 4}
                    textAnchor={i === 0 ? 'start' : i === days.length - 1 ? 'end' : 'middle'}
                    fill="var(--fg-muted)"
                    style={{ fontSize: 10, fontVariantNumeric: 'tabular-nums' }}
                  >
                    {shortDay(days[i]!.day)}
                  </text>
                ))}
            </>
          )}
        </svg>
      </div>

      <figcaption className="mt-2 text-[12px] text-fg-secondary">
        Spend per day over the last {days.length} days, in US dollars.
      </figcaption>

      <table data-testid="cost-table" className="sr-only">
        <caption>Spend per day</caption>
        <thead>
          <tr>
            <th scope="col">Day</th>
            <th scope="col">Cost</th>
            <th scope="col">Sessions</th>
          </tr>
        </thead>
        <tbody>
          {days.map((day) => (
            <tr key={day.day}>
              <th scope="row">{day.day}</th>
              <td>{money(day.usd)}</td>
              <td>{day.sessions}</td>
            </tr>
          ))}
        </tbody>
      </table>

      {hover && (
        <div
          data-testid="cost-tooltip"
          aria-hidden
          style={{ left: hover.left, top: hover.top }}
          className="pointer-events-none absolute z-10 w-[126px] rounded-[var(--radius-control)] border border-hairline bg-surface-3 px-2 py-1.5 shadow-[var(--shadow-popover)]"
        >
          <span className="block text-[11px] font-medium text-fg-primary">{shortDay(hover.day.day)}</span>
          <span className="block font-mono text-[11px] tabular-nums text-fg-secondary">
            {money(hover.day.usd)} · {hover.day.sessions} sessions
          </span>
        </div>
      )}
    </figure>
  )
}
