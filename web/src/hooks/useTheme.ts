// Theme state lives in a tiny zustand store (not React state) so every
// consumer - Shell, ThemeToggle, Palette's "Toggle theme" action, and
// useShortcuts' `t` binding - reads and writes the same value. `data-theme`
// on <html> and localStorage['styr.theme'] are applied synchronously as soon
// as this module loads (before first paint) to avoid a flash of the wrong
// theme; default is dark, matching "Design system" in the spec.
import { useEffect, useRef } from 'react'
import { api } from '../api/client'
import { useMe } from './useMe'
import { create } from 'zustand'

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'styr.theme'

function readInitialTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'dark' || stored === 'light') return stored
  } catch {
    // localStorage unavailable (private mode, disabled storage): fall back.
  }
  return 'dark'
}

function applyThemeAttr(theme: Theme) {
  document.documentElement.setAttribute('data-theme', theme)
}

interface ThemeStoreState {
  theme: Theme
  setTheme: (theme: Theme) => void
}

const useThemeStore = create<ThemeStoreState>((set) => ({
  theme: readInitialTheme(),
  setTheme: (theme) => {
    applyThemeAttr(theme)
    try {
      localStorage.setItem(STORAGE_KEY, theme)
    } catch {
      // Best-effort persistence only.
    }
    set({ theme })
  },
}))

// Apply immediately at module evaluation time (before React ever renders)
// so <html> never briefly shows the wrong theme.
applyThemeAttr(useThemeStore.getState().theme)

/** Read/write the current theme. Safe to call from any number of components. */
export function useTheme() {
  const theme = useThemeStore((s) => s.theme)
  const setTheme = useThemeStore((s) => s.setTheme)
  const toggleTheme = () => setTheme(theme === 'dark' ? 'light' : 'dark')
  return { theme, setTheme, toggleTheme }
}

/**
 * Mirrors theme changes to `me.prefs.theme` via PATCH /api/v1/me once a user
 * is logged in. Mount exactly once (Shell.tsx) - it fires a request per
 * theme change, and mounting it more than once would fire that request once
 * per mount.
 */
export function useThemeSync() {
  const { theme } = useTheme()
  const { data: me } = useMe()
  const isFirstRun = useRef(true)

  useEffect(() => {
    // Skip the mount-time run: that value either came from `me.prefs.theme`
    // already, or is the localStorage/default value we haven't changed yet.
    if (isFirstRun.current) {
      isFirstRun.current = false
      return
    }
    if (!me) return
    void api('/api/v1/me', { method: 'PATCH', json: { prefs: { ...me.prefs, theme } } }).catch(() => {
      // Best-effort: the local theme has already applied regardless.
    })
  }, [theme, me])
}
