// Timeline data derived from the folded transcript (lib/blocks.ts). Pure
// functions only - ActivityTimeline.tsx and ChangesList.tsx own rendering.
import type { Block } from './blocks'

export type Span = {
  id: string
  tool: string
  summary: string
  start: number
  end: number
  error: boolean
  lane: number
}

type ToolBlock = Extract<Block, { kind: 'tool' }>

function isToolBlock(block: Block): block is ToolBlock {
  return block.kind === 'tool'
}

/**
 * Converts tool blocks into timeline spans, with lanes assigned greedily:
 * spans are processed in start order, and each takes the lowest-numbered
 * lane whose most recently placed span has already ended by this span's
 * start time. Sequential (non-overlapping) spans collapse onto lane 0;
 * overlapping spans (e.g. concurrent tool calls) stack onto extra lanes.
 * An unresolved tool_use (no endedAt yet) becomes a zero-width open span at
 * its start time.
 */
export function toSpans(blocks: Block[]): { spans: Span[]; t0: number; t1: number } {
  const raw = blocks.filter(isToolBlock).map((b) => {
    const start = new Date(b.startedAt).getTime()
    const end = b.endedAt ? new Date(b.endedAt).getTime() : start
    return {
      id: b.id,
      tool: b.tool,
      summary: b.summary,
      start,
      end: Math.max(end, start),
      error: b.result?.isError === true,
    }
  })

  if (raw.length === 0) return { spans: [], t0: 0, t1: 0 }

  const ordered = [...raw].sort((a, b) => a.start - b.start)
  const laneEnds: number[] = [] // end time of the most recently placed span in each lane
  const spans: Span[] = ordered.map((s) => {
    let lane = laneEnds.findIndex((end) => end <= s.start)
    if (lane === -1) {
      lane = laneEnds.length
      laneEnds.push(s.end)
    } else {
      laneEnds[lane] = s.end
    }
    return { ...s, lane }
  })

  const t0 = Math.min(...raw.map((s) => s.start))
  const t1 = Math.max(...raw.map((s) => s.end))
  return { spans, t0, t1 }
}

type FileOp = 'read' | 'edit' | 'write'

const OP_BY_TOOL: Record<string, FileOp | undefined> = {
  Read: 'read',
  Edit: 'edit',
  MultiEdit: 'edit',
  Write: 'write',
}

function fileFromInput(input: unknown): string | null {
  if (input && typeof input === 'object' && 'file_path' in input) {
    const path = (input as Record<string, unknown>).file_path
    return typeof path === 'string' ? path : null
  }
  return null
}

/**
 * Files touched by Read/Edit/MultiEdit/Write tool calls, in first-seen
 * order, with ops deduplicated and merged per path.
 */
export function filesTouched(blocks: Block[]): { path: string; ops: FileOp[] }[] {
  const order: string[] = []
  const opsByPath = new Map<string, Set<FileOp>>()

  for (const block of blocks) {
    if (!isToolBlock(block)) continue
    const op = OP_BY_TOOL[block.tool]
    if (!op) continue
    const path = fileFromInput(block.input)
    if (!path) continue
    if (!opsByPath.has(path)) {
      opsByPath.set(path, new Set())
      order.push(path)
    }
    opsByPath.get(path)!.add(op)
  }

  return order.map((path) => ({ path, ops: [...opsByPath.get(path)!] }))
}
