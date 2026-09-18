// Opens one EventSource (or, under VITE_MOCK=1, one FakeEventSource) for the
// whole app and patches TanStack Query caches from its frames, per the
// Frontend conventions ("live updates: one EventSource on /api/v1/events in
// useLiveEvents() that patches query caches"). Mount this once near the root
// (AuthGate, once the user is known) rather than per page.
import { useEffect, useRef, useState } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import type {
  Approval,
  PipelineRunState,
  PipelineRunView,
  PipelineStatePayload,
  Session,
  SessionEvent,
  StepRunState,
  Workspace,
  WorkspaceState,
} from '../api/types'
import { usePartialsStore } from '../store/partials'
import { useLiveStatusStore } from '../store/live'

const isMock = import.meta.env.VITE_MOCK === '1'

const LIVE_KINDS = [
  'session.event',
  'session.state',
  'session.stats',
  'approval.created',
  'approval.decided',
  'workspace.state',
  'pipeline.state',
] as const
type LiveKind = (typeof LIVE_KINDS)[number]

interface BusMessage {
  kind: LiveKind
  // Every kind except workspace.state carries a session; that one instead
  // carries {id, state, error} directly in `payload` (see applyMessage).
  session_id?: string
  owner_id?: string | null
  seq?: number
  payload: unknown
}

interface WorkspaceStatePayload {
  id: string
  state: WorkspaceState
  error: string
}

interface StreamDelta {
  type: 'text_delta'
  text: string
}
interface StreamEvent {
  type: string
  delta?: StreamDelta
}
/** The raw Claude Code CLI envelope shape the session.event payload carries. */
interface CliEnvelope {
  type: string
  event?: StreamEvent
}

// Minimal shape both EventSource and FakeEventSource satisfy.
interface LiveMessageEvent {
  data: string
}
interface LiveSource {
  onopen: (() => void) | null
  onerror: (() => void) | null
  close: () => void
  addEventListener: (type: string, listener: (ev: LiveMessageEvent) => void) => void
  removeEventListener: (type: string, listener: (ev: LiveMessageEvent) => void) => void
}

function applySessionEvent(client: QueryClient, message: BusMessage) {
  const sessionId = message.session_id
  if (!sessionId) return
  const envelope = message.payload as CliEnvelope
  const delta = envelope?.type === 'stream_event' ? envelope.event : undefined
  if (delta?.type === 'content_block_delta' && delta.delta?.type === 'text_delta') {
    usePartialsStore.getState().append(sessionId, delta.delta.text)
    return
  }
  client.setQueryData<SessionEvent[]>(['session-events', sessionId], (prev) => {
    const seq = message.seq ?? (prev?.length ?? 0) + 1
    const event: SessionEvent = {
      id: seq,
      session_id: sessionId,
      seq,
      at: new Date().toISOString(),
      type: envelope?.type ?? 'raw',
      payload: message.payload,
    }
    return [...(prev ?? []), event]
  })
  if (envelope?.type === 'result') {
    usePartialsStore.getState().clear(sessionId)
  }
}

/** workspace.state carries no session_id - the row it patches lives in the
 * ['workspaces'] list cache, keyed by the workspace id in its own payload. */
function applyWorkspaceState(client: QueryClient, message: BusMessage) {
  const patch = message.payload as WorkspaceStatePayload
  if (!patch?.id) return
  client.setQueryData<Workspace[]>(['workspaces'], (prev) =>
    prev?.map((w) => (w.id === patch.id ? { ...w, state: patch.state, error: patch.error } : w)),
  )
}

/** `pipeline.state` carries no session either: it names a pipeline run and,
 * for a step's own move, the step run inside it. The run page reads
 * ['pipeline-run', id], so the frame patches that view in place rather than
 * refetching the whole graph on every step. */
function applyPipelineState(client: QueryClient, message: BusMessage) {
  const patch = message.payload as PipelineStatePayload
  if (!patch?.pipeline_run_id) return
  client.setQueryData<PipelineRunView>(['pipeline-run', patch.pipeline_run_id], (prev) => {
    if (!prev) return prev
    if (patch.step_run_id) {
      return {
        ...prev,
        steps: prev.steps.map((step) =>
          step.id === patch.step_run_id ? { ...step, state: patch.state as StepRunState } : step,
        ),
      }
    }
    return { ...prev, run: { ...prev.run, state: patch.state as PipelineRunState } }
  })
  // A step-level frame only moves one node; a run-level one changes which
  // runs the list shows, so that cache is refetched rather than guessed at.
  if (!patch.step_run_id) void client.invalidateQueries({ queryKey: ['pipeline-runs'] })
}

function applyMessage(client: QueryClient, kind: LiveKind, message: BusMessage) {
  switch (kind) {
    case 'session.event':
      applySessionEvent(client, message)
      return
    case 'session.state':
    case 'session.stats': {
      const sessionId = message.session_id
      if (!sessionId) return
      const patch = message.payload as Partial<Session>
      client.setQueryData<Session>(['session', sessionId], (prev) => (prev ? { ...prev, ...patch } : prev))
      client.setQueryData<Session[]>(['sessions'], (prev) => prev?.map((s) => (s.id === sessionId ? { ...s, ...patch } : s)))
      return
    }
    case 'approval.created':
    case 'approval.decided':
      void client.invalidateQueries({ queryKey: ['approvals'] })
      return
    case 'workspace.state':
      applyWorkspaceState(client, message)
      return
    case 'pipeline.state':
      applyPipelineState(client, message)
      return
  }
}

async function openSource(): Promise<LiveSource> {
  if (isMock) {
    const { FakeEventSource } = await import('../mocks/fakeEventSource')
    return new FakeEventSource('/api/v1/events') as unknown as LiveSource
  }
  return new EventSource('/api/v1/events', { withCredentials: true }) as unknown as LiveSource
}

export function useLiveEvents() {
  const queryClient = useQueryClient()
  const [connected, setConnected] = useState(false)
  const sourceRef = useRef<LiveSource | null>(null)

  // Document title reflects the pending-approvals count, kept in sync
  // whenever that query's cache changes (initial load, refetch after
  // approval.created/decided invalidation, or a manual refetch).
  useEffect(() => {
    return queryClient.getQueryCache().subscribe((event) => {
      if (event.query.queryKey[0] !== 'approvals') return
      const data = event.query.state.data as Approval[] | undefined
      if (!data) return
      document.title = data.length > 0 ? `(${data.length}) Styr` : 'Styr'
    })
  }, [queryClient])

  useEffect(() => {
    let cancelled = false
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let attempt = 0

    async function connect() {
      const source = await openSource()
      if (cancelled) {
        source.close()
        return
      }
      sourceRef.current = source
      source.onopen = () => {
        attempt = 0
        setConnected(true)
        useLiveStatusStore.getState().setConnected(true)
      }
      source.onerror = () => {
        setConnected(false)
        useLiveStatusStore.getState().setConnected(false)
        source.close()
        if (cancelled) return
        const delay = Math.min(30_000, 1000 * 2 ** attempt)
        attempt += 1
        retryTimer = setTimeout(() => void connect(), delay)
      }
      for (const kind of LIVE_KINDS) {
        source.addEventListener(kind, (ev) => {
          let message: BusMessage
          try {
            message = JSON.parse(ev.data) as BusMessage
          } catch {
            return
          }
          applyMessage(queryClient, kind, message)
        })
      }
    }

    void connect()
    return () => {
      cancelled = true
      if (retryTimer) clearTimeout(retryTimer)
      sourceRef.current?.close()
      sourceRef.current = null
      useLiveStatusStore.getState().setConnected(false)
    }
  }, [queryClient])

  return { connected }
}
