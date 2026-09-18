// Subscribes to a CSS media query from React. The review surface needs this
// because the two diff layouts are different DOM, not different CSS: a
// side-by-side row pairs an old line with a new one, a unified row does not,
// so the breakpoint has to be readable in JS. Everything else in Styr stays on
// plain Tailwind breakpoints.
import { useSyncExternalStore } from 'react'

function subscribe(query: string) {
  return (onChange: () => void) => {
    const list = window.matchMedia(query)
    list.addEventListener('change', onChange)
    return () => list.removeEventListener('change', onChange)
  }
}

export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    subscribe(query),
    () => window.matchMedia(query).matches,
    // Server/prerender fallback: the narrow layout, which is the one that
    // works at any width.
    () => false,
  )
}
