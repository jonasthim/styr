// App shell for every authenticated route: rail or tab bar (by viewport),
// the Cmd/Ctrl+K palette, the shortcuts cheat sheet, and the global keyboard
// registry. Mounted once in router.tsx's `_app` layout route, inside
// AuthGate, so `me` is already loaded by the time this renders.
import { useEffect, useState, type ReactNode } from 'react'
import clsx from 'clsx'
import { useShortcuts } from '../../hooks/useShortcuts'
import { useThemeSync } from '../../hooks/useTheme'
import { useUiStore } from '../../store/ui'
import { Rail } from './Rail'
import { TabBar } from './TabBar'
import { Palette } from './Palette'
import { ShortcutsDialog } from './ShortcutsDialog'

// Matches the rail/tab-bar breakpoint from the design spec (Rail behaviour:
// "Below 900 px the rail becomes a bottom TabBar").
const DESKTOP_QUERY = '(min-width: 900px)'

function useIsDesktopNav(): boolean {
  const [isDesktop, setIsDesktop] = useState(
    () => typeof window !== 'undefined' && window.matchMedia(DESKTOP_QUERY).matches,
  )

  useEffect(() => {
    const mql = window.matchMedia(DESKTOP_QUERY)
    const onChange = () => setIsDesktop(mql.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  return isDesktop
}

export function Shell({ children }: { children: ReactNode }) {
  useShortcuts()
  useThemeSync()
  const isDesktop = useIsDesktopNav()
  const paletteOpen = useUiStore((s) => s.paletteOpen)
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen)
  const shortcutsOpen = useUiStore((s) => s.shortcutsOpen)
  const setShortcutsOpen = useUiStore((s) => s.setShortcutsOpen)

  return (
    <div className="min-h-screen bg-canvas">
      {isDesktop ? <Rail /> : <TabBar />}
      {/* A flex column, so a page can hand its content area `flex-1` and
          centre an empty state in it instead of boxing it at the top. */}
      <main className={clsx('flex min-h-screen min-w-0 flex-col', isDesktop ? 'pl-14' : 'pb-14')}>{children}</main>
      <Palette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
    </div>
  )
}
