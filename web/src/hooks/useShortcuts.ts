// Global keyboard shortcut registry (mount once, in Shell.tsx). Bindings:
//   Cmd/Ctrl+K  toggle the command palette (works even while typing)
//   g i / g s / g w  go to Inbox / Sessions / Workspaces
//   n           new session (-> /sessions?new=1)
//   ?           open the shortcuts cheat sheet
//   [           pin/unpin the rail
//   t           toggle theme
// Every binding below Cmd/Ctrl+K is ignored while focus is in an
// input/textarea/contenteditable, or while the palette itself is open (its
// own cmdk keydown handling owns the keyboard then).
import { useEffect, useRef } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useUiStore } from '../store/ui'
import { useTheme } from './useTheme'

const G_CHORD_TIMEOUT_MS = 900

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  return target.tagName === 'INPUT' || target.tagName === 'TEXTAREA'
}

export function useShortcuts() {
  const navigate = useNavigate()
  const { toggleTheme } = useTheme()
  const paletteOpen = useUiStore((s) => s.paletteOpen)
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen)
  const setShortcutsOpen = useUiStore((s) => s.setShortcutsOpen)
  const toggleRailPinned = useUiStore((s) => s.toggleRailPinned)
  const pendingG = useRef(false)
  const pendingGTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    function clearPendingG() {
      pendingG.current = false
      if (pendingGTimer.current) {
        clearTimeout(pendingGTimer.current)
        pendingGTimer.current = null
      }
    }

    function goTo(path: '/inbox' | '/sessions' | '/workspaces') {
      void navigate({ to: path })
    }

    function newSession() {
      void navigate({ to: '/sessions', search: { new: 1 } })
    }

    function onKeyDown(event: KeyboardEvent) {
      const modKey = event.metaKey || event.ctrlKey
      if (modKey && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setPaletteOpen(!paletteOpen)
        return
      }

      if (paletteOpen || isTypingTarget(event.target) || event.metaKey || event.ctrlKey || event.altKey) {
        return
      }

      if (pendingG.current) {
        clearPendingG()
        if (event.key === 'i') goTo('/inbox')
        else if (event.key === 's') goTo('/sessions')
        else if (event.key === 'w') goTo('/workspaces')
        return
      }

      switch (event.key) {
        case 'g':
          pendingG.current = true
          pendingGTimer.current = setTimeout(clearPendingG, G_CHORD_TIMEOUT_MS)
          return
        case 'n':
          newSession()
          return
        case '?':
          setShortcutsOpen(true)
          return
        case '[':
          toggleRailPinned()
          return
        case 't':
          toggleTheme()
          return
      }
    }

    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      clearPendingG()
    }
  }, [navigate, paletteOpen, setPaletteOpen, setShortcutsOpen, toggleRailPinned, toggleTheme])
}
