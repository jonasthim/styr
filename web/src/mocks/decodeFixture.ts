// Decodes the raw Claude Code CLI stream-json lines recorded in
// internal/harness/claude/testdata/*.jsonl (and copied into ./fixtures/) into
// the shape the real Styr backend's harness codec produces: JSON of
// internal/harness.Event (see internal/harness/claude/testdata/PROTOCOL.md and
// internal/harness/types.go). Used to seed a mock session's
// GET /api/v1/sessions/:id/events response with realistic transcript data so
// Task 20's e2e spec exercises the same payload shape lib/blocks.ts's
// foldEvents expects from the real API, not the raw CLI wire protocol.
//
// Only the subset of the protocol fixture 02 actually exercises is handled:
// system/init, assistant text/tool_use content blocks, user/tool_result,
// and the final result line. Everything else (hooks, stream_event deltas,
// thinking blocks, status/rate-limit lines, the operator's own echoed turn)
// decodes to null and is dropped, matching PROTOCOL.md's description of what
// the real codec ignores or drops.
import type { HarnessEventPayload } from '../lib/blocks'

interface RawContentBlock {
  type: string
  text?: string
  id?: string
  name?: string
  input?: unknown
  tool_use_id?: string
  content?: unknown
  is_error?: boolean
}

interface RawLine {
  type: string
  subtype?: string
  cwd?: string
  session_id?: string
  tools?: string[]
  model?: string
  message?: { content?: RawContentBlock[] | string; role?: string }
  parent_tool_use_id?: string | null
  total_cost_usd?: number
  duration_ms?: number
  num_turns?: number
  is_error?: boolean
  result?: string
  usage?: { input_tokens?: number; output_tokens?: number }
}

function decodeAssistant(obj: RawLine): HarnessEventPayload | null {
  const content = obj.message?.content
  if (!Array.isArray(content) || content.length === 0) return null
  const block = content[0]
  if (block.type === 'text') {
    return { Type: 'text', Text: block.text ?? '' }
  }
  if (block.type === 'tool_use') {
    return {
      Type: 'tool_use',
      ToolUse: {
        ID: block.id ?? '',
        Name: block.name ?? '',
        Input: block.input ?? {},
        ParentToolUseID: obj.parent_tool_use_id ?? '',
      },
    }
  }
  return null // thinking, or any other block type: codec ignores it
}

function decodeUser(obj: RawLine): HarnessEventPayload | null {
  const content = obj.message?.content
  if (typeof content === 'string') return null // echo of our own turn: no event
  if (!Array.isArray(content)) return null
  const block = content.find((b) => b.type === 'tool_result')
  if (!block) return null
  return {
    Type: 'tool_result',
    ToolResult: {
      ToolUseID: block.tool_use_id ?? '',
      Content: typeof block.content === 'string' ? block.content : JSON.stringify(block.content ?? ''),
      IsError: block.is_error ?? false,
    },
  }
}

function decodeLine(raw: string): HarnessEventPayload | null {
  if (raw.startsWith('>>> ')) return null // our own stdin line, not CLI output
  let obj: RawLine
  try {
    obj = JSON.parse(raw) as RawLine
  } catch {
    return null
  }
  switch (obj.type) {
    case 'system':
      if (obj.subtype !== 'init') return null
      return {
        Type: 'init',
        Init: { SessionID: obj.session_id ?? '', Model: obj.model ?? '', Tools: obj.tools ?? [] },
      }
    case 'assistant':
      return decodeAssistant(obj)
    case 'user':
      return decodeUser(obj)
    case 'result':
      return {
        Type: 'result',
        Result: {
          Subtype: obj.subtype ?? '',
          IsError: obj.is_error ?? false,
          NumTurns: obj.num_turns ?? 0,
          CostUSD: obj.total_cost_usd ?? 0,
          InputTokens: obj.usage?.input_tokens ?? 0,
          OutputTokens: obj.usage?.output_tokens ?? 0,
          DurationMS: obj.duration_ms ?? 0,
          Text: obj.result ?? '',
        },
      }
    default:
      // hook_started/hook_response/status, stream_event, rate_limit_event,
      // control_request/control_response: not modelled as transcript events.
      return null
  }
}

/** Decodes a whole `.jsonl?raw` fixture into harness.Event-shaped payloads, in order. */
export function decodeFixtureEvents(fixtureRaw: string, startAt: Date): HarnessEventPayload[] {
  const events: HarnessEventPayload[] = []
  let offsetMs = 0
  for (const line of fixtureRaw.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const decoded = decodeLine(trimmed)
    if (!decoded) continue
    offsetMs += 400
    decoded.At = new Date(startAt.getTime() + offsetMs).toISOString()
    events.push(decoded)
  }
  return events
}
