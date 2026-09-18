// Theme state lives in a tiny zustand store (not React state) so every
// consumer - Shell, ThemeToggle, Palette's "Toggle theme" action, and
// useShortcuts' `t` binding - reads and writes the same value. `data-theme`
// on <html> and localStorage['styr.theme'] are applied synchronously as soon
// as this module loads (before first paint) to avoid a flash of the wrong
// theme; default is dark, matching "Design system" in the spec.
//
// The persisted value is a *mode* - 'dark', 'light' or 'system' - not just
// the two concrete themes: 'system' tracks the OS preference via
// matchMedia('(prefers-color-scheme: dark)') and re-resolves whenever it
// changes, instead of being a one-time snapshot taken when chosen. `theme`
// (what actually gets applied to <html> and what ThemeToggle/the `t`
// shortcut read) is always the *resolved* concrete value; `mode` is the
// persisted selection, surfaced for Profile's three-way selector. Toggling
// (ThemeToggle, `t`, the palette action) always lands on an explicit
// dark/light mode - "system" is only ever chosen from Profile's selector.
import { useEffect, useRef } from 'react'
import { api } from '../api/client'
import { useMe } from './useMe'
import { create } from 'zustand'

export type Theme = 'dark' | 'light'
export type ThemeMode = Theme | 'system'

const STORAGE_KEY = 'styr.theme'

/** Pure: what concrete theme a mode resolves to, given the OS preference. */
export function resolveTheme(mode: ThemeMode, prefersDark: boolean): Theme {
  if (mode === 'system') return prefersDark ? 'dark' : 'light'
  return mode
}

function readInitialMode(): ThemeMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'dark' || stored === 'light' || stored === 'system') return stored
  } catch {
    // localStorage unavailable (private mode, disabled storage): fall back.
  }
  return 'dark'
}

function systemPrefersDark(): boolean {
  try {
    return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true
  } catch {
    // No matchMedia (non-browser environment, e.g. a unit test): default
    // to the module's overall dark-first default.
    return true
  }
}

function applyThemeAttr(theme: Theme) {
  try {
    document.documentElement.setAttribute('data-theme', theme)
  } catch {
    // No DOM (non-browser environment, e.g. a unit test): nothing to apply.
  }
}

interface ThemeStoreState {
  mode: ThemeMode
  theme: Theme
  setMode: (mode: ThemeMode) => void
}

const useThemeStore = create<ThemeStoreState>((set) => {
  const initialMode = readInitialMode()
  return {
    mode: initialMode,
    theme: resolveTheme(initialMode, systemPrefersDark()),
    setMode: (mode) => {
      const theme = resolveTheme(mode, systemPrefersDark())
      applyThemeAttr(theme)
      try {
        localStorage.setItem(STORAGE_KEY, mode)
      } catch {
        // Best-effort persistence only.
      }
      set({ mode, theme })
    },
  }
})

// Apply immediately at module evaluation time (before React ever renders)
// so <html> never briefly shows the wrong theme.
applyThemeAttr(useThemeStore.getState().theme)

// Keep 'system' mode in sync with OS-level changes (e.g. the desktop
// switching from light to dark while Styr is open in the background). A
// change while mode is an explicit 'dark'/'light' is ignored - the user
// picked a concrete theme on purpose.
try {
  window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener('change', (event) => {
    const { mode } = useThemeStore.getState()
    if (mode !== 'system') return
    const theme = resolveTheme(mode, event.matches)
    applyThemeAttr(theme)
    useThemeStore.setState({ theme })
  })
} catch {
  // No matchMedia (non-browser environment): system mode just won't
  // live-update, which only matters where there is no OS to change anyway.
}

/** Read/write the current theme. Safe to call from any number of components. */
export function useTheme() {
  const theme = useThemeStore((s) => s.theme)
  const mode = useThemeStore((s) => s.mode)
  const setMode = useThemeStore((s) => s.setMode)
  const setTheme = (theme: Theme) => setMode(theme)
  const toggleTheme = () => setMode(theme === 'dark' ? 'light' : 'dark')
  return { theme, mode, setTheme, setMode, toggleTheme }
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
