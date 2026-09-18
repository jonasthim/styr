// SVG timeline of tool spans (lib/spans.ts): time on x, one row per lane,
// spans 10px tall coloured by tool tier. Hovering a span shows its summary
// and duration; clicking jumps the transcript to that block by reusing the
// #b-<id> anchor mechanism Transcript.tsx already listens for. While the
// session is running the right edge advances every second so the timeline
// visibly keeps pace; the CSS transition that animates it is disabled
// globally under prefers-reduced-motion (styles/base.css).
import { useEffect, useMemo, useState } from 'react'
import * as Tooltip from '@radix-ui/react-tooltip'
import { toSpans, type Span } from '../../lib/spans'
import type { Block } from '../../lib/blocks'

const ROW_H = 18
const SPAN_H = 10
const VIEW_W = 1000
const MIN_SPAN_W = 6
const MIN_RANGE_MS = 1000

type Tier = 'read' | 'write' | 'exec'

const TIER_BY_TOOL: Record<string, Tier> = {
  Read: 'read',
  Grep: 'read',
  Glob: 'read',
  WebFetch: 'read',
  WebSearch: 'read',
  Edit: 'write',
  MultiEdit: 'write',
  Write: 'write',
  Bash: 'exec',
}

function colorFor(span: Span): string {
  if (span.error) return 'var(--state-failed)'
  switch (TIER_BY_TOOL[span.tool] ?? 'read') {
    case 'write':
      return 'var(--accent)'
    case 'exec':
      return 'var(--state-attention)'
    default:
      return 'var(--fg-muted)'
  }
}

function formatDuration(ms: number): string {
  if (ms <= 0) return 'running'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function formatClock(t: number): string {
  return new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

/** Jumps the transcript to a block, reusing the #b-<id> hash Transcript.tsx watches. */
function jumpToBlock(id: string) {
  if (window.location.hash === `#${id}`) {
    window.dispatchEvent(new HashChangeEvent('hashchange'))
  } else {
    window.location.hash = id
  }
}

export function ActivityTimeline({ blocks, running }: { blocks: Block[]; running: boolean }) {
  const { spans, t0, t1: dataT1 } = useMemo(() => toSpans(blocks), [blocks])
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!running) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [running])

  if (spans.length === 0) {
    return <div className="py-6 text-center text-[13px] text-fg-muted">No tool activity yet.</div>
  }

  const t1 = running ? Math.max(dataT1, now) : dataT1
  const range = Math.max(t1 - t0, MIN_RANGE_MS)
  const laneCount = spans.reduce((max, s) => Math.max(max, s.lane + 1), 1)
  const height = laneCount * ROW_H + 4

  function xFor(t: number): number {
    return ((t - t0) / range) * VIEW_W
  }

  return (
    <Tooltip.Provider delayDuration={150}>
      <svg
        role="img"
        aria-label="Tool activity timeline"
        viewBox={`0 0 ${VIEW_W} ${height}`}
        preserveAspectRatio="none"
        className="w-full transition-[width] duration-150"
        style={{ height }}
      >
        {spans.map((s) => {
          const x = xFor(s.start)
          const w = Math.max(xFor(s.end) - x, MIN_SPAN_W)
          const y = s.lane * ROW_H + (ROW_H - SPAN_H) / 2
          return (
            <Tooltip.Root key={s.id}>
              <Tooltip.Trigger asChild>
                <rect
                  data-testid="span"
                  tabIndex={0}
                  role="button"
                  aria-label={`${s.tool}: ${s.summary}`}
                  x={x}
                  y={y}
                  width={w}
                  height={SPAN_H}
                  rx={2}
                  fill={colorFor(s)}
                  className="cursor-pointer outline-none focus-visible:opacity-80"
                  onClick={() => jumpToBlock(s.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      jumpToBlock(s.id)
                    }
                  }}
                />
              </Tooltip.Trigger>
              <Tooltip.Portal>
                <Tooltip.Content
                  side="top"
                  sideOffset={6}
                  className="z-50 max-w-[240px] rounded-[var(--radius-1)] border border-hairline bg-surface-3 px-2 py-1.5 text-[12px] text-fg-primary shadow-lg"
                >
                  <div className="truncate font-mono">{s.summary}</div>
                  <div className="font-mono text-[11px] tabular-nums text-fg-muted">
                    {formatDuration(s.end - s.start)}
                  </div>
                </Tooltip.Content>
              </Tooltip.Portal>
            </Tooltip.Root>
          )
        })}
      </svg>
      <div className="mt-2 flex items-center justify-between font-mono text-[11px] tabular-nums text-fg-muted">
        <span>{formatClock(t0)}</span>
        {running && <span className="text-state-running">live</span>}
        <span>{formatClock(t1)}</span>
      </div>
    </Tooltip.Provider>
  )
}
