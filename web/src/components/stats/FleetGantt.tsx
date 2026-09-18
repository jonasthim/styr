// The fleet Gantt: one lane per session, the window's worth of time across
// it, and what the session was doing in each slice. Hand-rolled SVG on the
// design tokens - no chart library, no raw colour.
//
// Encoding, in the terms the plan sets: running is the state-running fill,
// waiting is the attention colour *and* a 45° hatch (so it never depends on
// colour alone), idle is a thin muted rule rather than a block - a session
// doing nothing should not carry the same weight as one that is working.
// The right 4% of the plot is left empty on purpose: it gives the "now" line
// somewhere to sit, so the live edge reads as an edge.
import { useRef, useState, type CSSProperties } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import type { GanttKind, GanttLane, GanttSegment } from '../../api/types'
import { useElementWidth } from '../../hooks/useElementWidth'

const LANE_H = 28
const BAR_H = 12
const IDLE_H = 3
const AXIS_H = 22
/** Fraction of the plot the window occupies; the rest is the live edge. */
const PLOT_FRACTION = 0.96
const HATCH_ID = 'styr-gantt-waiting-hatch'
const LABEL_COL = 'w-[104px] shrink-0 sm:w-[180px]'

const KIND_LABEL: Record<GanttKind, string> = {
  running: 'Running',
  waiting: 'Waiting on you',
  idle: 'Idle',
}

function formatDuration(ms: number): string {
  const minutes = Math.round(ms / 60_000)
  if (minutes < 1) return 'under a minute'
  if (minutes < 60) return `${minutes} min`
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest ? `${hours} h ${rest} min` : `${hours} h`
}

function clock(at: number): string {
  return new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

/** Tick spacing that keeps the axis to a handful of labels whatever the
 * window: ten minutes for an hour, an hour for six, four hours for a day. */
function tickStep(hours: number): number {
  if (hours <= 1) return 10 * 60_000
  if (hours <= 6) return 60 * 60_000
  return 4 * 3_600_000
}

function ticks(from: number, to: number, hours: number): number[] {
  const step = tickStep(hours)
  const out: number[] = []
  for (let t = Math.ceil(from / step) * step; t <= to; t += step) out.push(t)
  return out
}

function laneSummary(lane: GanttLane): string {
  const totals = new Map<GanttKind, number>()
  for (const segment of lane.segments) {
    const ms = Date.parse(segment.end) - Date.parse(segment.start)
    totals.set(segment.kind, (totals.get(segment.kind) ?? 0) + ms)
  }
  const parts = [...totals.entries()].map(([kind, ms]) => `${KIND_LABEL[kind].toLowerCase()} ${formatDuration(ms)}`)
  return `${lane.title}, owned by ${lane.owner}: ${parts.join(', ')}.`
}

function LegendSwatch({ kind }: { kind: GanttKind }) {
  if (kind === 'idle') return <span aria-hidden className="h-[3px] w-3.5 rounded-full bg-state-idle opacity-70" />
  return (
    <svg width={14} height={10} aria-hidden className="block">
      <rect
        width={14}
        height={10}
        rx={2}
        fill={kind === 'waiting' ? `url(#${HATCH_ID})` : 'var(--state-running)'}
      />
    </svg>
  )
}

interface Hover {
  left: number
  top: number
  title: string
  detail: string
}

export function FleetGantt({ lanes, hours }: { lanes: GanttLane[]; hours: number }) {
  const navigate = useNavigate()
  const panelRef = useRef<HTMLDivElement | null>(null)
  const [plotRef, plotWidth] = useElementWidth<HTMLDivElement>()
  const [hover, setHover] = useState<Hover | null>(null)

  const to = Date.now()
  const from = to - hours * 3_600_000
  const span = to - from
  const usable = plotWidth * PLOT_FRACTION
  const x = (at: number) => ((at - from) / span) * usable

  function showHover(event: { clientX: number; clientY: number }, title: string, detail: string) {
    const rect = panelRef.current?.getBoundingClientRect()
    if (!rect) return
    setHover({
      left: Math.min(Math.max(8, event.clientX - rect.left + 12), Math.max(8, rect.width - 200)),
      top: Math.max(4, event.clientY - rect.top - 44),
      title,
      detail,
    })
  }

  function geometry(segment: GanttSegment) {
    const start = Math.max(from, Date.parse(segment.start))
    const end = Math.min(to, Date.parse(segment.end))
    if (end <= start) return null
    const left = x(start)
    const height = segment.kind === 'idle' ? IDLE_H : BAR_H
    return {
      x: left,
      y: (LANE_H - height) / 2,
      width: Math.max(2, x(end) - left),
      height,
      detail: `${formatDuration(end - start)} · ${clock(start)}–${clock(end)}`,
    }
  }

  return (
    <div
      ref={panelRef}
      data-testid="fleet-gantt"
      className="relative mt-4 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]"
      onMouseLeave={() => setHover(null)}
    >
      <svg width={0} height={0} aria-hidden className="absolute">
        <defs>
          {/* 45° hatch: waiting is the one state a reader must never have to
              pick out by hue alone. */}
          <pattern id={HATCH_ID} width={6} height={6} patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <rect width={6} height={6} fill="var(--state-attention)" fillOpacity={0.24} />
            <line x1={0} y1={0} x2={0} y2={6} stroke="var(--state-attention)" strokeWidth={2.5} />
          </pattern>
        </defs>
      </svg>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-b border-hairline bg-surface-2 px-3 py-2">
        {(['running', 'waiting', 'idle'] as GanttKind[]).map((kind) => (
          <span key={kind} className="flex items-center gap-1.5 text-[11px] text-fg-secondary">
            <LegendSwatch kind={kind} />
            {KIND_LABEL[kind]}
          </span>
        ))}
        <span className="ml-auto font-mono text-[11px] tabular-nums text-fg-muted">
          {clock(from)} – {clock(to)}
        </span>
      </div>

      <div className="flex items-stretch">
        <div className={LABEL_COL} />
        <div ref={plotRef} className="min-w-0 flex-1">
          <svg width="100%" height={AXIS_H} aria-hidden className="block">
            {plotWidth > 0 &&
              ticks(from, to, hours).map((t) => (
                <g key={t}>
                  <line x1={x(t)} y1={AXIS_H - 4} x2={x(t)} y2={AXIS_H} className="stroke-hairline" strokeWidth={1} />
                  <text x={x(t)} y={AXIS_H - 8} textAnchor="middle" fill="var(--fg-muted)" style={{ fontSize: 10 }}>
                    {clock(t)}
                  </text>
                </g>
              ))}
          </svg>
        </div>
      </div>

      {lanes.map((lane) => (
        <div key={lane.session_id} data-testid="gantt-lane" className="flex items-stretch border-t border-hairline">
          <div className={`${LABEL_COL} flex items-center pl-3 pr-2`}>
            <Link
              to="/sessions/$id"
              params={{ id: lane.session_id }}
              data-testid="gantt-lane-link"
              title={`${lane.title} — ${lane.owner}`}
              className="min-w-0 truncate font-mono text-[11px] text-fg-secondary no-underline outline-none hover:text-fg-primary focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-[var(--ring)]"
            >
              {lane.title}
            </Link>
          </div>
          <div className="min-w-0 flex-1">
            <span className="sr-only">{laneSummary(lane)}</span>
            <svg width="100%" height={LANE_H} className="block" aria-hidden>
              {plotWidth > 0 &&
                lane.segments.map((segment) => {
                  const box = geometry(segment)
                  if (!box) return null
                  return (
                    <rect
                      key={`${segment.kind}-${segment.start}`}
                      data-testid="gantt-segment"
                      data-kind={segment.kind}
                      x={box.x}
                      y={box.y}
                      width={box.width}
                      height={box.height}
                      rx={segment.kind === 'idle' ? 1.5 : 3}
                      fill={
                        segment.kind === 'waiting'
                          ? `url(#${HATCH_ID})`
                          : segment.kind === 'running'
                            ? 'var(--state-running)'
                            : 'var(--state-idle)'
                      }
                      fillOpacity={segment.kind === 'idle' ? 0.5 : 1}
                      className="cursor-pointer"
                      onMouseEnter={(e) => showHover(e, `${KIND_LABEL[segment.kind]} · ${lane.title}`, box.detail)}
                      onMouseMove={(e) => showHover(e, `${KIND_LABEL[segment.kind]} · ${lane.title}`, box.detail)}
                      onClick={() => void navigate({ to: '/sessions/$id', params: { id: lane.session_id } })}
                    >
                      <title>{`${KIND_LABEL[segment.kind]} — ${box.detail}`}</title>
                    </rect>
                  )
                })}
            </svg>
          </div>
        </div>
      ))}

      {/* The live edge, drawn over every lane at once. Its x is the plot's
          own right edge, so it moves with the measured plot rather than
          being guessed at from the panel width. */}
      {plotWidth > 0 && (
        <div
          aria-hidden
          style={{ '--now-x': `${usable}px` } as CSSProperties}
          className="pointer-events-none absolute bottom-0 top-[60px] left-[calc(104px+var(--now-x))] w-px bg-accent/50 sm:left-[calc(180px+var(--now-x))]"
        >
          <span className="absolute left-1 top-0 font-mono text-[9px] leading-none text-accent">now</span>
        </div>
      )}

      {hover && (
        <div
          data-testid="gantt-tooltip"
          aria-hidden
          style={{ left: hover.left, top: hover.top }}
          className="pointer-events-none absolute z-10 w-[190px] rounded-[var(--radius-control)] border border-hairline bg-surface-3 px-2 py-1.5 shadow-[var(--shadow-popover)]"
        >
          <span className="block truncate text-[11px] font-medium text-fg-primary">{hover.title}</span>
          <span className="block font-mono text-[11px] tabular-nums text-fg-secondary">{hover.detail}</span>
        </div>
      )}
    </div>
  )
}
