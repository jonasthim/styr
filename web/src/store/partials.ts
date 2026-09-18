import { create } from 'zustand'

// Streaming text deltas live outside TanStack Query: they mutate on every
// partial frame (multiple times a second) and query-cache updates would
// force every subscriber to re-render on each keystroke of model output.
// Session views select their own session id's buffer instead.
interface PartialsState {
  bySession: Record<string, string>
  append: (sessionId: string, text: string) => void
  clear: (sessionId: string) => void
}

export const usePartialsStore = create<PartialsState>((set) => ({
  bySession: {},
  append: (sessionId, text) =>
    set((state) => ({
      bySession: { ...state.bySession, [sessionId]: (state.bySession[sessionId] ?? '') + text },
    })),
  clear: (sessionId) =>
    set((state) => {
      if (!(sessionId in state.bySession)) return state
      const next = { ...state.bySession }
      delete next[sessionId]
      return { bySession: next }
    }),
}))
