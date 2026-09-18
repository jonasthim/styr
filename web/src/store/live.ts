// Mirrors useLiveEvents()'s `connected` flag into zustand so components
// outside the hook's own call site (the Rail's live dot) can read it
// reactively. useLiveEvents() must only be called once (it owns the single
// EventSource for the app - see its own header comment), so this store is
// the read path for everyone else instead of calling the hook again.
import { create } from 'zustand'

interface LiveStatusState {
  connected: boolean
  setConnected: (connected: boolean) => void
}

export const useLiveStatusStore = create<LiveStatusState>((set) => ({
  connected: false,
  setConnected: (connected) => set({ connected }),
}))
