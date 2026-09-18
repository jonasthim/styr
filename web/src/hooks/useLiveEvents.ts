// Opens one EventSource (or, under VITE_MOCK=1, one FakeEventSource) for the
// whole app and patches TanStack Query caches from its frames, per the
// Frontend conventions ("live updates: one EventSource on /api/v1/events in
// useLiveEvents() that patches query caches"). Mount this once near the root
// (AuthGate, once the user is known) rather than per page.
import { useEffect, useRef, useState } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import type { Approval, Session, SessionEvent } from '../api/types'
import { usePartialsStore } from '../store/partials'

const isMock = import.meta.env.VITE_MOCK === '1'

const LIVE_KINDS = ['session.event', 'session.state', 'session.stats', 'approval.created', 'approval.decided'] as const
type LiveKind = (typeof LIVE_KINDS)[number]

interface BusMessage {
  kind: LiveKind
  session_id: string
  owner_id: string | null
  seq?: number
  payload: unknown
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
  const envelope = message.payload as CliEnvelope
  const delta = envelope?.type === 'stream_event' ? envelope.event : undefined
  if (delta?.type === 'content_block_delta' && delta.delta?.type === 'text_delta') {
    usePartialsStore.getState().append(message.session_id, delta.delta.text)
    return
  }
  client.setQueryData<SessionEvent[]>(['session-events', message.session_id], (prev) => {
    const seq = message.seq ?? (prev?.length ?? 0) + 1
    const event: SessionEvent = {
      id: seq,
      session_id: message.session_id,
      seq,
      at: new Date().toISOString(),
      type: envelope?.type ?? 'raw',
      payload: message.payload,
    }
    return [...(prev ?? []), event]
  })
  if (envelope?.type === 'result') {
    usePartialsStore.getState().clear(message.session_id)
  }
}

function applyMessage(client: QueryClient, kind: LiveKind, message: BusMessage) {
  switch (kind) {
    case 'session.event':
      applySessionEvent(client, message)
      return
    case 'session.state':
    case 'session.stats': {
      const patch = message.payload as Partial<Session>
      client.setQueryData<Session>(['session', message.session_id], (prev) => (prev ? { ...prev, ...patch } : prev))
      client.setQueryData<Session[]>(['sessions'], (prev) =>
        prev?.map((s) => (s.id === message.session_id ? { ...s, ...patch } : s)),
      )
      return
    }
    case 'approval.created':
    case 'approval.decided':
      void client.invalidateQueries({ queryKey: ['approvals'] })
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
      }
      source.onerror = () => {
        setConnected(false)
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
    }
  }, [queryClient])

  return { connected }
}
