// Cross-component shell UI state: whether the rail is pinned open, and
// whether the command palette / shortcuts cheat sheet are open. Lives in
// zustand (not component state) because useShortcuts.ts drives all three
// from a single global keydown listener that isn't a descendant of every
// component that needs to read them.
import { create } from 'zustand'

interface UiState {
  railPinned: boolean
  paletteOpen: boolean
  shortcutsOpen: boolean
  setRailPinned: (pinned: boolean) => void
  toggleRailPinned: () => void
  setPaletteOpen: (open: boolean) => void
  setShortcutsOpen: (open: boolean) => void
}

export const useUiStore = create<UiState>((set, get) => ({
  railPinned: false,
  paletteOpen: false,
  shortcutsOpen: false,
  setRailPinned: (railPinned) => set({ railPinned }),
  toggleRailPinned: () => set({ railPinned: !get().railPinned }),
  setPaletteOpen: (paletteOpen) => set({ paletteOpen }),
  setShortcutsOpen: (shortcutsOpen) => set({ shortcutsOpen }),
}))
