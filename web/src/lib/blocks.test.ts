import { describe, expect, it } from 'vitest'
import { foldEvents } from './blocks'
import type { SessionEvent } from '../api/types'

// Builds a SessionEvent whose payload is JSON of internal/harness/types.go's
// Event struct (Go field names — the struct has no json tags, so encoding/json
// marshals PascalCase keys as-is). See internal/harness/types.go and
// internal/harness/claude/testdata/PROTOCOL.md.
function ev(seq: number, at: string, payload: Record<string, unknown>): SessionEvent {
  return { id: seq, session_id: 's1', seq, at, type: String(payload.Type), payload }
}

describe('foldEvents', () => {
  it('pairs a tool_use with its matching tool_result into one tool block', () => {
    const events = [
      ev(1, '2026-01-01T00:00:00Z', {
        Type: 'tool_use',
        At: '2026-01-01T00:00:00Z',
        ToolUse: { ID: 'toolu_1', Name: 'Read', Input: { file_path: '/tmp/note.txt' }, ParentToolUseID: '' },
      }),
      ev(2, '2026-01-01T00:00:01Z', {
        Type: 'tool_result',
        At: '2026-01-01T00:00:01Z',
        ToolResult: { ToolUseID: 'toolu_1', Content: '1\thello\n2\t', IsError: false },
      }),
    ]

    const blocks = foldEvents(events)

    expect(blocks).toHaveLength(1)
    expect(blocks[0]).toMatchObject({
      kind: 'tool',
      tool: 'Read',
      startedAt: '2026-01-01T00:00:00Z',
      endedAt: '2026-01-01T00:00:01Z',
      result: { content: '1\thello\n2\t', isError: false },
    })
  })

  it('leaves an unpaired tool_use open (no result)', () => {
    const events = [
      ev(1, '2026-01-01T00:00:00Z', {
        Type: 'tool_use',
        At: '2026-01-01T00:00:00Z',
        ToolUse: { ID: 'toolu_2', Name: 'Bash', Input: { command: 'echo hi' }, ParentToolUseID: '' },
      }),
    ]

    const blocks = foldEvents(events)

    expect(blocks).toHaveLength(1)
    expect(blocks[0].kind).toBe('tool')
    if (blocks[0].kind === 'tool') {
      expect(blocks[0].result).toBeUndefined()
      expect(blocks[0].endedAt).toBeUndefined()
    }
  })

  it('carries turns and cost on the result block', () => {
    const events = [
      ev(1, '2026-01-01T00:00:02Z', {
        Type: 'result',
        At: '2026-01-01T00:00:02Z',
        Result: {
          Subtype: 'success',
          IsError: false,
          NumTurns: 3,
          CostUSD: 0.4624,
          InputTokens: 34,
          OutputTokens: 111,
          DurationMS: 4321,
          Text: 'hello',
        },
      }),
    ]

    const blocks = foldEvents(events)

    expect(blocks).toHaveLength(1)
    expect(blocks[0]).toMatchObject({ kind: 'result', subtype: 'success', turns: 3, cost: 0.4624 })
  })

  it('preserves event order across mixed block kinds', () => {
    const events = [
      ev(1, '2026-01-01T00:00:00Z', { Type: 'text', At: '2026-01-01T00:00:00Z', Text: 'first' }),
      ev(2, '2026-01-01T00:00:01Z', {
        Type: 'tool_use',
        At: '2026-01-01T00:00:01Z',
        ToolUse: { ID: 'toolu_3', Name: 'Read', Input: {}, ParentToolUseID: '' },
      }),
      ev(3, '2026-01-01T00:00:02Z', {
        Type: 'tool_result',
        At: '2026-01-01T00:00:02Z',
        ToolResult: { ToolUseID: 'toolu_3', Content: 'ok', IsError: false },
      }),
      ev(4, '2026-01-01T00:00:03Z', { Type: 'text', At: '2026-01-01T00:00:03Z', Text: 'second' }),
    ]

    const blocks = foldEvents(events)

    expect(blocks.map((b) => b.kind)).toEqual(['text', 'tool', 'text'])
    expect(blocks[0]).toMatchObject({ text: 'first' })
    expect(blocks[2]).toMatchObject({ text: 'second' })
  })

  it('skips unknown/unhandled event types', () => {
    const events = [
      ev(1, '2026-01-01T00:00:00Z', { Type: 'init', At: '2026-01-01T00:00:00Z', Init: { SessionID: 's1', Model: 'm', Tools: [] } }),
      ev(2, '2026-01-01T00:00:01Z', { Type: 'raw', At: '2026-01-01T00:00:01Z', Raw: { foo: 'bar' } }),
      ev(3, '2026-01-01T00:00:02Z', { Type: 'exit', At: '2026-01-01T00:00:02Z', ExitCode: 0 }),
      ev(4, '2026-01-01T00:00:03Z', { Type: 'partial', At: '2026-01-01T00:00:03Z', Text: 'partial text' }),
      ev(5, '2026-01-01T00:00:04Z', { Type: 'permission_request', At: '2026-01-01T00:00:04Z', Permission: { RequestID: 'r1', ToolName: 'Bash', Input: {}, ToolUseID: 't1' } }),
      ev(6, '2026-01-01T00:00:05Z', { Type: 'text', At: '2026-01-01T00:00:05Z', Text: 'only this' }),
    ]

    const blocks = foldEvents(events)

    expect(blocks).toHaveLength(1)
    expect(blocks[0]).toMatchObject({ kind: 'text', text: 'only this' })
  })

  it('renders a mock-synthesised user event (POST /messages) as a user block', () => {
    const events = [ev(1, '2026-01-01T00:00:00Z', { Type: 'user', At: '2026-01-01T00:00:00Z', Text: 'again' })]

    const blocks = foldEvents(events)

    expect(blocks).toEqual([{ kind: 'user', id: expect.any(String), text: 'again', at: '2026-01-01T00:00:00Z' }])
  })
})
