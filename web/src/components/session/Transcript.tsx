// Virtualised transcript: one row per Block (lib/blocks.ts). Auto-scrolls to
// the bottom while the reader is already there; otherwise shows a "Jump to
// latest" pill instead of yanking the viewport. A URL hash (#b-<id>) scrolls
// to and briefly highlights that block on mount or hash change.
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowDown } from 'lucide-react'
import type { Block } from '../../lib/blocks'
import { TextBlock } from './TextBlock'
import { ToolBlock } from './ToolBlock'
import { PartialText } from './PartialText'

const BOTTOM_THRESHOLD_PX = 48
const HIGHLIGHT_MS = 1600

function formatCost(cost: number): string {
  return `$${cost.toFixed(2)}`
}

function formatClock(at: string): string {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

function Row({ block, highlighted }: { block: Block; highlighted: boolean }) {
  if (block.kind === 'tool') {
    return <ToolBlock block={block} highlighted={highlighted} />
  }

  if (block.kind === 'text') {
    return (
      <div id={block.id} className={highlighted ? 'rounded-[var(--radius-1)] bg-surface-2/60' : undefined}>
        <TextBlock text={block.text} />
      </div>
    )
  }

  if (block.kind === 'user') {
    return (
      <div id={block.id} className={highlighted ? 'rounded-[var(--radius-1)] bg-surface-2/60' : undefined}>
        <div className="mb-1! flex items-baseline gap-2">
          <span className="text-[11px] font-medium text-fg-muted">You</span>
          <span className="font-mono text-[11px] tabular-nums text-fg-muted">{formatClock(block.at)}</span>
        </div>
        <div className="max-w-[72ch] whitespace-pre-wrap text-[13px] leading-6 text-fg-primary">{block.text}</div>
      </div>
    )
  }

  // result
  return (
    <div id={block.id} className={highlighted ? 'rounded-[var(--radius-1)] bg-surface-2/60' : undefined}>
      <div className="flex items-center gap-2 py-1.5! text-[12px] text-fg-muted">
        <span className="h-px flex-1 bg-hairline" />
        <span className="font-mono tabular-nums">
          Turn finished: {block.turns} {block.turns === 1 ? 'turn' : 'turns'}, {formatCost(block.cost)}
        </span>
        <span className="h-px flex-1 bg-hairline" />
      </div>
    </div>
  )
}

export function Transcript({ blocks, sessionId }: { blocks: Block[]; sessionId: string }) {
  const parentRef = useRef<HTMLDivElement>(null)
  const atBottomRef = useRef(true)
  const [showJump, setShowJump] = useState(false)
  const [highlightId, setHighlightId] = useState<string | null>(null)
  const prevCount = useRef(0)

  const virtualizer = useVirtualizer({
    count: blocks.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 56,
    overscan: 8,
  })

  function scrollToBottom(behavior: ScrollBehavior = 'auto') {
    const el = parentRef.current
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    atBottomRef.current = true
    setShowJump(false)
  }

  // Land on a deep-linked block (#b-<id>) once blocks are available; otherwise
  // start at the bottom, the natural "latest activity" position for a log.
  useLayoutEffect(() => {
    if (blocks.length === 0) return
    const hash = window.location.hash.slice(1)
    if (hash) {
      const index = blocks.findIndex((b) => b.id === hash)
      if (index >= 0) {
        virtualizer.scrollToIndex(index, { align: 'center' })
        setHighlightId(hash)
        const t = setTimeout(() => setHighlightId(null), HIGHLIGHT_MS)
        return () => clearTimeout(t)
      }
    }
    scrollToBottom()
    // Deep-link landing / initial bottom scroll only needs to run once blocks first arrive.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [blocks.length > 0])

  useEffect(() => {
    function onHashChange() {
      const hash = window.location.hash.slice(1)
      if (!hash) return
      const index = blocks.findIndex((b) => b.id === hash)
      if (index < 0) return
      virtualizer.scrollToIndex(index, { align: 'center' })
      setHighlightId(hash)
      const t = setTimeout(() => setHighlightId(null), HIGHLIGHT_MS)
      return () => clearTimeout(t)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [blocks, virtualizer])

  // New blocks: stay pinned to the bottom if the reader was already there;
  // otherwise surface the "Jump to latest" pill instead of moving them.
  useEffect(() => {
    if (blocks.length > prevCount.current) {
      if (atBottomRef.current) {
        requestAnimationFrame(() => scrollToBottom())
      } else {
        setShowJump(true)
      }
    }
    prevCount.current = blocks.length
    // eslint-disable-next-line react-hooks/exhaustive-deps -- scrollToBottom is stable enough for this effect's purpose
  }, [blocks.length])

  function onScroll() {
    const el = parentRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD_PX
    atBottomRef.current = atBottom
    if (atBottom) setShowJump(false)
  }

  return (
    <div className="relative min-h-0 flex-1">
      <div ref={parentRef} onScroll={onScroll} data-testid="transcript" className="h-full overflow-y-auto px-4! py-3!">
        <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
          {virtualizer.getVirtualItems().map((row) => (
            <div
              key={row.key}
              data-index={row.index}
              ref={virtualizer.measureElement}
              style={{ position: 'absolute', top: 0, left: 0, width: '100%', transform: `translateY(${row.start}px)` }}
              className="pb-2!"
            >
              <Row block={blocks[row.index]} highlighted={blocks[row.index].id === highlightId} />
            </div>
          ))}
        </div>
        <PartialText sessionId={sessionId} />
      </div>
      {showJump && (
        <button
          type="button"
          onClick={() => scrollToBottom('smooth')}
          className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-hairline bg-surface-2 px-3! py-1.5! text-[12px] font-medium text-fg-primary shadow-lg transition-transform duration-150 hover:-translate-y-0.5"
        >
          <ArrowDown size={13} />
          Jump to latest
        </button>
      )}
    </div>
  )
}
