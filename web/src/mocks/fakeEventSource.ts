// msw@2 cannot intercept EventSource (it has no fetch/XHR under the hood), so
// mock mode swaps the real EventSource for this fake in useLiveEvents.ts. It
// replays the lines of fixture 01 (internal/harness/claude/testdata, copied
// to ./fixtures/01_simple_text.jsonl) as `session.event` bus frames, one line
// every 100 ms, looping forever, against a fixed fake session id so the demo
// session view has something to render.
import fixtureRaw from './fixtures/01_simple_text.jsonl?raw'

export const MOCK_EVENT_SESSION_ID = '00000000-0000-4000-8000-000000000003'

interface BusMessage {
  kind: string
  session_id: string
  owner_id: string | null
  seq: number
  payload: unknown
}

const FIXTURE_LINES = fixtureRaw
  .split('\n')
  .map((l) => l.trim())
  .filter((l) => l.length > 0 && !l.startsWith('>>> '))

export interface FakeMessageEvent {
  type: string
  data: string
}

type Listener = (ev: FakeMessageEvent) => void

// Every live FakeEventSource (in practice, at most one - useLiveEvents.ts
// mounts a single connection at the app root) registers here, so
// emitFakeEvent below can reach it from a msw handler module that has no
// reference of its own to whichever instance is currently open.
const activeInstances = new Set<FakeEventSource>()

/** Delivers an arbitrary frame to every open FakeEventSource, the same way a
 * real backend push would arrive over SSE. Used by handlers.ts to exercise
 * useLiveEvents.ts's live-patch path (e.g. `workspace.state`) for frames this
 * fixture-replay class doesn't generate on its own timer. */
export function emitFakeEvent(type: string, data: unknown): void {
  const frame: FakeMessageEvent = { type, data: JSON.stringify(data) }
  for (const instance of activeInstances) instance.dispatch(type, frame)
}

/** Drop-in replacement for the subset of EventSource that useLiveEvents uses. */
export class FakeEventSource {
  readyState = 0 // CONNECTING
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private listeners = new Map<string, Set<Listener>>()
  private timer: ReturnType<typeof setInterval> | null = null
  private line = 0

  constructor(_url: string) {
    activeInstances.add(this)
    setTimeout(() => {
      this.readyState = 1 // OPEN
      this.onopen?.()
      this.timer = setInterval(() => this.emitNext(), 100)
    }, 0)
  }

  addEventListener(type: string, listener: Listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(listener)
  }

  removeEventListener(type: string, listener: Listener) {
    this.listeners.get(type)?.delete(listener)
  }

  dispatch(type: string, frame: FakeMessageEvent) {
    for (const listener of this.listeners.get(type) ?? []) listener(frame)
  }

  close() {
    this.readyState = 2 // CLOSED
    if (this.timer) clearInterval(this.timer)
    this.timer = null
    activeInstances.delete(this)
  }

  private emitNext() {
    if (FIXTURE_LINES.length === 0) return
    const raw = FIXTURE_LINES[this.line % FIXTURE_LINES.length]
    this.line += 1
    let payload: unknown
    try {
      payload = JSON.parse(raw)
    } catch {
      return
    }
    const message: BusMessage = {
      kind: 'session.event',
      session_id: MOCK_EVENT_SESSION_ID,
      owner_id: null,
      seq: this.line,
      payload,
    }
    const frame: FakeMessageEvent = { type: 'session.event', data: JSON.stringify(message) }
    for (const listener of this.listeners.get('session.event') ?? []) listener(frame)
  }
}
