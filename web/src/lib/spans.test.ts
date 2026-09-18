import { describe, expect, it } from 'vitest'
import { toSpans, filesTouched } from './spans'
import type { Block } from './blocks'

type ToolBlock = Extract<Block, { kind: 'tool' }>

function tool(
  id: string,
  toolName: string,
  startedAt: string,
  endedAt: string | undefined,
  input: unknown = {},
  isError = false,
): ToolBlock {
  return {
    kind: 'tool',
    id,
    tool: toolName,
    input,
    summary: toolName,
    startedAt,
    endedAt,
    result: endedAt ? { content: '', isError } : undefined,
  }
}

describe('toSpans', () => {
  it('assigns two overlapping spans to lanes 0 and 1', () => {
    const blocks: Block[] = [
      tool('b-1', 'Bash', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:05.000Z'),
      tool('b-2', 'Bash', '2026-01-01T00:00:01.000Z', '2026-01-01T00:00:03.000Z'),
    ]

    const { spans } = toSpans(blocks)

    expect(spans.find((s) => s.id === 'b-1')?.lane).toBe(0)
    expect(spans.find((s) => s.id === 'b-2')?.lane).toBe(1)
  })

  it('collapses sequential (non-overlapping) spans onto lane 0', () => {
    const blocks: Block[] = [
      tool('b-1', 'Read', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:01.000Z'),
      tool('b-2', 'Read', '2026-01-01T00:00:02.000Z', '2026-01-01T00:00:03.000Z'),
      tool('b-3', 'Read', '2026-01-01T00:00:03.000Z', '2026-01-01T00:00:04.000Z'),
    ]

    const { spans, t0, t1 } = toSpans(blocks)

    expect(spans.every((s) => s.lane === 0)).toBe(true)
    expect(t0).toBe(new Date('2026-01-01T00:00:00.000Z').getTime())
    expect(t1).toBe(new Date('2026-01-01T00:00:04.000Z').getTime())
  })

  it('marks a span error when its tool result was an error', () => {
    const blocks: Block[] = [tool('b-1', 'Bash', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:01.000Z', {}, true)]

    const { spans } = toSpans(blocks)

    expect(spans[0]).toMatchObject({ id: 'b-1', error: true })
  })

  it('treats an unpaired tool_use (no endedAt) as a zero-width open span', () => {
    const blocks: Block[] = [tool('b-1', 'Bash', '2026-01-01T00:00:00.000Z', undefined)]

    const { spans } = toSpans(blocks)

    expect(spans[0]).toMatchObject({ id: 'b-1', lane: 0 })
    expect(spans[0].end).toBe(spans[0].start)
  })

  it('ignores non-tool blocks and returns a zero range when there are no tool blocks', () => {
    const blocks: Block[] = [{ kind: 'text', id: 'b-1', text: 'hi', at: '2026-01-01T00:00:00.000Z' }]

    expect(toSpans(blocks)).toEqual({ spans: [], t0: 0, t1: 0 })
  })
})

describe('filesTouched', () => {
  it('reports Read, Edit and Write inputs as their respective ops', () => {
    const blocks: Block[] = [
      tool('b-1', 'Read', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:01.000Z', { file_path: 'a.ts' }),
      tool('b-2', 'Edit', '2026-01-01T00:00:01.000Z', '2026-01-01T00:00:02.000Z', { file_path: 'b.ts' }),
      tool('b-3', 'MultiEdit', '2026-01-01T00:00:02.000Z', '2026-01-01T00:00:03.000Z', { file_path: 'd.ts' }),
      tool('b-4', 'Write', '2026-01-01T00:00:03.000Z', '2026-01-01T00:00:04.000Z', { file_path: 'c.ts' }),
    ]

    expect(filesTouched(blocks)).toEqual([
      { path: 'a.ts', ops: ['read'] },
      { path: 'b.ts', ops: ['edit'] },
      { path: 'd.ts', ops: ['edit'] },
      { path: 'c.ts', ops: ['write'] },
    ])
  })

  it('deduplicates the same path across multiple calls, merging ops', () => {
    const blocks: Block[] = [
      tool('b-1', 'Read', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:01.000Z', { file_path: 'note.txt' }),
      tool('b-2', 'Edit', '2026-01-01T00:00:01.000Z', '2026-01-01T00:00:02.000Z', { file_path: 'note.txt' }),
      tool('b-3', 'Edit', '2026-01-01T00:00:02.000Z', '2026-01-01T00:00:03.000Z', { file_path: 'note.txt' }),
    ]

    expect(filesTouched(blocks)).toEqual([{ path: 'note.txt', ops: ['read', 'edit'] }])
  })

  it('ignores tool calls without a file_path and tools that do not touch files', () => {
    const blocks: Block[] = [
      tool('b-1', 'Bash', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:01.000Z', { command: 'ls' }),
      tool('b-2', 'Read', '2026-01-01T00:00:01.000Z', '2026-01-01T00:00:02.000Z', {}),
    ]

    expect(filesTouched(blocks)).toEqual([])
  })
})
