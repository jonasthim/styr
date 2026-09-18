// Folds a flat SessionEvent list (as returned by GET /api/v1/sessions/:id/events
// and appended to the ['session-events', id] query cache by useLiveEvents.ts)
// into the renderable transcript blocks Transcript.tsx virtualises.
//
// SessionEvent.payload is JSON of internal/harness.Event (Go: internal/harness/types.go).
// That struct has no `json:` tags, so encoding/json marshals its exported field
// names as-is — PascalCase, not camelCase. The shapes below mirror it field for
// field: Type, At, Init, Text, ToolUse{ID,Name,Input,ParentToolUseID},
// ToolResult{ToolUseID,Content,IsError}, Permission, Result{Subtype,IsError,
// NumTurns,CostUSD,InputTokens,OutputTokens,DurationMS,Text}, Raw, ExitCode, Err.
//
// One exception: harness.EventType has no "user" value — the harness only
// decodes what the CLI emits, and the CLI's echo of the operator's own turn is
// deliberately dropped by the codec (PROTOCOL.md, "Styr's DecodeLine must
// special-case this"). To show what the operator typed in the transcript, the
// mock's POST /messages handler (web/src/mocks/handlers.ts) synthesises a
// SessionEvent with payload.Type "user" — a mock-only convention, not a real
// harness.EventType — which foldEvents renders as a `user` block.
import type { SessionEvent } from '../api/types'

export type Block =
  | { kind: 'text'; id: string; text: string; at: string }
  | {
      kind: 'tool'
      id: string
      tool: string
      input: unknown
      summary: string
      result?: { content: string; isError: boolean }
      startedAt: string
      endedAt?: string
    }
  | { kind: 'result'; id: string; subtype: string; turns: number; cost: number; at: string }
  | { kind: 'user'; id: string; text: string; at: string }

interface HarnessInit {
  SessionID: string
  Model: string
  Tools: string[]
}

interface HarnessToolUse {
  ID: string
  Name: string
  Input: unknown
  ParentToolUseID: string
}

interface HarnessToolResult {
  ToolUseID: string
  Content: string
  IsError: boolean
}

interface HarnessPermission {
  RequestID: string
  ToolName: string
  Input: unknown
  ToolUseID: string
}

interface HarnessResult {
  Subtype: string
  IsError: boolean
  NumTurns: number
  CostUSD: number
  InputTokens: number
  OutputTokens: number
  DurationMS: number
  Text: string
}

/** JSON shape of internal/harness.Event, as it arrives in SessionEvent.payload. */
export interface HarnessEventPayload {
  Type: string
  At?: string
  Init?: HarnessInit | null
  Text?: string
  ToolUse?: HarnessToolUse | null
  ToolResult?: HarnessToolResult | null
  Permission?: HarnessPermission | null
  Result?: HarnessResult | null
  Raw?: unknown
  ExitCode?: number
  Err?: string
}

function isPayload(value: unknown): value is HarnessEventPayload {
  return typeof value === 'object' && value !== null && 'Type' in value
}

/** Header-row summary text for a tool block; ToolBlock.tsx overrides the Bash case with the command. */
function summarize(name: string, input: unknown): string {
  if (input && typeof input === 'object') {
    const obj = input as Record<string, unknown>
    if (typeof obj.command === 'string') return obj.command
    if (typeof obj.file_path === 'string') return obj.file_path
    if (typeof obj.path === 'string') return obj.path
  }
  try {
    const json = JSON.stringify(input)
    if (!json) return name
    return json.length > 120 ? `${json.slice(0, 120)}…` : json
  } catch {
    return name
  }
}

export function foldEvents(events: SessionEvent[]): Block[] {
  const blocks: Block[] = []
  // Index of open tool blocks (tool_use seen, no tool_result yet) by ToolUse.ID,
  // so a later tool_result can be folded into the same block in place.
  const openToolIndex = new Map<string, number>()

  const ordered = [...events].sort((a, b) => a.seq - b.seq)

  for (const event of ordered) {
    const payload = event.payload
    if (!isPayload(payload)) continue
    const at = payload.At ?? event.at

    switch (payload.Type) {
      case 'text': {
        blocks.push({ kind: 'text', id: `b-${event.id}`, text: payload.Text ?? '', at })
        break
      }

      case 'tool_use': {
        const tu = payload.ToolUse
        if (!tu) break
        const index = blocks.length
        blocks.push({
          kind: 'tool',
          id: `b-${tu.ID}`,
          tool: tu.Name,
          input: tu.Input,
          summary: summarize(tu.Name, tu.Input),
          startedAt: at,
        })
        openToolIndex.set(tu.ID, index)
        break
      }

      case 'tool_result': {
        const tr = payload.ToolResult
        if (!tr) break
        const index = openToolIndex.get(tr.ToolUseID)
        if (index === undefined) break // orphan result with no preceding tool_use: nothing to fold into
        const block = blocks[index]
        if (block.kind !== 'tool') break
        blocks[index] = { ...block, result: { content: tr.Content, isError: tr.IsError }, endedAt: at }
        openToolIndex.delete(tr.ToolUseID)
        break
      }

      case 'result': {
        const r = payload.Result
        if (!r) break
        blocks.push({ kind: 'result', id: `b-${event.id}`, subtype: r.Subtype, turns: r.NumTurns, cost: r.CostUSD, at })
        break
      }

      case 'user': {
        // Mock-only convention (see module header): not a real harness.EventType.
        blocks.push({ kind: 'user', id: `b-${event.id}`, text: payload.Text ?? '', at })
        break
      }

      // 'init', 'partial', 'permission_request', 'raw', 'exit', and anything
      // else are not transcript blocks: init feeds SessionHeader, partial
      // feeds PartialText/the partials store, permission_request feeds
      // PermissionCard via the approvals API, raw/exit are not shown.
      default:
        break
    }
  }

  return blocks
}
